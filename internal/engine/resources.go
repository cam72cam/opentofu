package engine

import (
	"context"
	"sync"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/configs/hcl2shim"
	"github.com/opentofu/opentofu/internal/providers"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type ResourceInstances map[addrs.InstanceKey]ValuePromise
type Resource struct {
	ValuePromise
	instances *Promise[ResourceInstances]
}

func NewResource(ctx context.Context, addr addrs.AbsResource, config *configs.Resource, scope *Scope) Resource {
	if scope.op == walkValidate {
		return Resource{ValuePromise: NewPromise(addr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
			abstract, diags := tofuNodeAbstractResource(addr.Config(), config, scope)
			node := tofu.NodeValidatableResource{&abstract}
			evalCtx := scope.EvalContext(self)

			// HACK Wire in provider
			providerDiags := providersHack(ctx, evalCtx, scope, abstract.Provider())
			diags = diags.Append(providerDiags)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			resolvedProvider := tofu.ResolvedProvider{
				// For validate, we can just depend on the unconfigured root instance
				ProviderConfig: addrs.AbsProviderConfig{Provider: abstract.Provider()},
			}
			abstract.SetProvider(resolvedProvider)

			execDiags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkValidate))
			diags = diags.Append(execDiags)

			// TODO this mirrors tofu/evaluate.go, but should be made a *lot* smarter as we know the output schema + if there are any count/for_each wrappers applied
			return cty.DynamicVal, diags
		})}
	}
	expansion := NewPromise(addr, func(self executor) (ResourceInstances, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		abstract, diags := tofuNodeAbstractResource(addr.Config(), config, scope)
		writeDiags := abstract.WriteResourceState(evalCtx, addr) // Oddly named, but this performs the expansion (for now)
		diags = diags.Append(writeDiags)

		instances := ResourceInstances{}

		// Some of the state manipulation here is doing pieces of states.Module.SetResourceInstanceCurrent
		for _, resAddr := range evalCtx.InstanceExpander().ExpandResource(addr) {
			resAddr := resAddr
			key := resAddr.Resource.Key
			instances[key] = NewPromise(resAddr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
				return NewResourceInstance(ctx, resAddr, config, self, scope)
			})
		}
		return instances, diags
	})

	outputValue := NewPromise(&addr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
		// expansion
		expanded, diags := expansion.Value(self)

		instances := make(map[addrs.InstanceKey]cty.Value)
		for key, mod := range expanded {
			var modDiags tfdiags.Diagnostics
			instances[key], modDiags = mod.Value(self)
			diags = diags.Append(modDiags)
		}
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		// Lifted from tofu/evaluate.go

		switch {
		case config.Count != nil:
			length := -1
			for key := range instances {
				intKey, ok := key.(addrs.IntKey)
				if !ok {
					// old key from state which is being dropped
					continue
				}
				if int(intKey) >= length {
					length = int(intKey) + 1
				}
			}

			if length <= 0 {
				return cty.EmptyTupleVal, diags
			}
			vals := make([]cty.Value, length)

			for key, instance := range instances {
				intKey, ok := key.(addrs.IntKey)
				if !ok {
					// old key from state which is being dropped
					continue
				}

				vals[int(intKey)] = instance
			}

			// Insert unknown values where there are any missing instances
			for i, v := range vals {
				if v.IsNull() {
					vals[i] = cty.DynamicVal
					continue
				}
			}
			return cty.TupleVal(vals), diags

		case config.ForEach != nil:
			instanceMap := make(map[string]cty.Value)
			for key, mod := range instances {
				sk := key.(addrs.StringKey)
				instanceMap[string(sk)] = mod
			}
			return cty.ObjectVal(instanceMap), diags
		default:
			return instances[addrs.NoKey], diags
		}
	})

	return Resource{outputValue, expansion}
}

func (m Resource) Expand(c *ConcurrencyPool) tfdiags.Diagnostics {
	if m.instances == nil {
		return nil
	}
	expanded, diags := m.instances.Value(nil)

	for _, res := range expanded {
		c.Add(res)
	}
	return diags
}

var (
	// Until we have proper providers wired in to this system, this is a hack for testing unconfigured providers
	runningProviders = map[addrs.Provider]providers.Interface{}
	providersLock    sync.Mutex
	numRequests      = 0
)

func providersHack(ctx context.Context, evalCtx tofu.EvalContext, scope *Scope, provider addrs.Provider) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	var err error
	mock := evalCtx.(*tofu.MockEvalContext)
	mock.ProviderSchemaSchema, err = scope.Plugins.ProviderSchema(provider)
	if err != nil {
		return diags.Append(err)
	}
	// TODO use actually resolved provider
	providersLock.Lock()
	defer providersLock.Unlock()
	if _, ok := runningProviders[provider]; !ok {
		println("START")
		runningProviders[provider], err = scope.Plugins.NewProviderInstance(provider)
		if err != nil {
			return diags.Append(err)
		}
		// TODO HACK actually configure resolved provider
		{
			configSchema, _ := scope.Plugins.ProviderConfigSchema(provider)
			configBody := hcl2shim.SynthBody(provider.String(), make(map[string]cty.Value))
			configVal, _, evalDiags := evalCtx.EvaluateBlock(configBody, configSchema, nil, tofu.EvalDataForNoInstanceKey)
			if evalDiags.HasErrors() {
				return diags.Append(evalDiags)
			}

			cpr := runningProviders[provider].ConfigureProvider(ctx, providers.ConfigureProviderRequest{
				TerraformVersion: "1.10.0",
				Config:           configVal,
			})
			diags = diags.Append(cpr.Diagnostics)
			if diags.HasErrors() {
				return diags
			}
		}
	}
	mock.ProviderProvider = runningProviders[provider]
	return diags
}

func tofuNodeAbstractResource(addr addrs.ConfigResource, config *configs.Resource, scope *Scope) (tofu.NodeAbstractResource, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	abstract := tofu.NodeAbstractResource{
		Addr: addr,

		// Set from GraphNodeTargetable
		// TODO Targets []addrs.Targetable

		// Set from GraphNodeTargetable
		// TODO Excludes []addrs.Targetable

		// This resource may expand into instances which need to be imported.
		// TODO importTargets []*ImportTarget

		// generateConfigPath tells this node which file to write generated config
		// into. If empty, then config should not be generated.
		// TODO generateConfigPath string

		// removedBlockProvisioners holds any possibly existing configs.Provisioner configs that could be defined by using
		// removed.provisioner configuration. If the field "Config.Managed.Provisioners" is having no provisioners, then
		// these provisioners should be used instead.
		// TODO removedBlockProvisioners []*configs.Provisioner
	}

	// AttachResourceConfigTransformer
	abstract.AttachResourceConfig(config)

	// AttachSchemaTransformer
	schema, schemaVersion, err := scope.Plugins.ResourceTypeSchema(abstract.Provider(), addr.Resource.Mode, addr.Resource.Type)
	if err != nil {
		diags = diags.Append(err)
	}
	abstract.AttachResourceSchema(schema, schemaVersion)

	// TODO
	// AttachProviderMetaConfigs(config.moduleConfig.Module.ProviderMetas)
	// AttachProvisionerSchema
	// AttachDataResourceDependsOn

	return abstract, diags

}

func NewResourceInstance(ctx context.Context, addr addrs.AbsResourceInstance, config *configs.Resource, self executor, scope *Scope) (cty.Value, tfdiags.Diagnostics) {
	evalCtx := scope.EvalContext(self)

	// Create pre-expansion node for embedding
	abstract, diags := tofuNodeAbstractResource(addr.ConfigResource(), config, scope)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	abstractInstance := &tofu.NodeAbstractResourceInstance{
		NodeAbstractResource: abstract,

		Addr: addr,

		//TODO preDestroyRefresh bool

		// During import we may generate configuration for a resource, which needs
		// to be stored in the final change.
		//TODO generatedConfigHCL string
	}

	if state := evalCtx.PrevRunState().Resource(addr.ContainingResource()); state != nil && state.Instance(addr.Resource.Key) != nil {
		abstractInstance.AttachResourceState(state)
	}

	//TODO How does this work cross module? node.Dependencies []addrs.ConfigResource

	// Now that we have attached the config and state, we can resolve the requested provider
	// TransformProvider
	providedBy := abstractInstance.ProvidedBy()
	resolvedProvider := tofu.ResolvedProvider{
		// TODO provider configs AbsProviderConfig for configured providers
		ProviderConfig: addrs.AbsProviderConfig{
			// Module:
			Provider: abstractInstance.Provider(),
			//Alias:
		},
		KeyExpression: providedBy.KeyExpression,
		KeyModule:     providedBy.KeyModule,
		KeyResource:   providedBy.KeyResource,
		KeyExact:      providedBy.KeyExact,
	}
	abstractInstance.SetProvider(resolvedProvider)

	// Wire in provider
	providerDiags := providersHack(ctx, evalCtx, scope, abstract.Provider())
	diags = diags.Append(providerDiags)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	// Execute
	if scope.op == walkPlan {
		node := tofu.NodePlannableResourceInstance{
			NodeAbstractResourceInstance: abstractInstance,
			//TODO ForceCreateBeforeDestroy bool

			// skipRefresh indicates that we should skip refreshing individual instances
			//TODO skipRefresh bool

			// skipPlanChanges indicates we should skip trying to plan change actions
			// for any instances.
			//TODO skipPlanChanges bool

			// forceReplace are resource instance addresses where the user wants to
			// force generating a replace action. This set isn't pre-filtered, so
			// it might contain addresses that have nothing to do with the resource
			// that this node represents, which the node itself must therefore ignore.
			//TODO forceReplace []addrs.AbsResourceInstance

			// replaceTriggeredBy stores references from replace_triggered_by which
			// triggered this instance to be replaced.
			//TODO replaceTriggeredBy []*addrs.Reference

			// importTarget, if populated, contains the information necessary to plan
			// an import of this resource.
			//TODO importTarget EvaluatedConfigImportTarget
		}
		diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(scope.op)))
	}
	if scope.op == walkApply {
		node := tofu.NodeApplyableResourceInstance{
			NodeAbstractResourceInstance: abstractInstance,
			//TODO ForceCreateBeforeDestroy bool

			// skipRefresh indicates that we should skip refreshing individual instances
			//TODO skipRefresh bool

			// skipApplyChanges indicates we should skip trying to apply change actions
			// for any instances.
			//TODO skipApplyChanges bool

			// forceReplace are resource instance addresses where the user wants to
			// force generating a replace action. This set isn't pre-filtered, so
			// it might contain addresses that have nothing to do with the resource
			// that this node represents, which the node itself must therefore ignore.
			//TODO forceReplace []addrs.AbsResourceInstance

			// replaceTriggeredBy stores references from replace_triggered_by which
			// triggered this instance to be replaced.
			//TODO replaceTriggeredBy []*addrs.Reference

			// importTarget, if populated, contains the information necessary to apply
			// an import of this resource.
			//TODO importTarget EvaluatedConfigImportTarget
		}
		diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(scope.op)))
	}

	if src := evalCtx.State().ResourceInstanceObject(addr, states.CurrentGen); src != nil {
		ty := abstract.Schema.ImpliedType()
		val, valDiags := src.Decode(ty)
		diags = diags.Append(valDiags)
		return val.Value, diags
	}

	return cty.NilVal, diags
}
