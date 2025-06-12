package engine

import (
	"context"
	"sync"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/configs/hcl2shim"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/providers"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewResourceValidate(ctx context.Context, addr addrs.AbsResource, config *configs.Resource, scope *Scope) *Promise[cty.Value] {
	value := NewPromise[cty.Value](addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
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
	})
	return value
}

type ResourcePlanData struct {
	Values  map[addrs.InstanceKey]cty.Value
	Changes []*plans.ResourceInstanceChangeSrc
	PrevRun *states.Resource
	Refresh *states.Resource
	State   *states.Resource
}

type ResourcePlan struct {
	*Promise[cty.Value]
	Data *Promise[ResourcePlanData]
}

func NewResourcePlan(ctx context.Context, addr addrs.AbsResource, config *configs.Resource, prior *states.Resource, scope *Scope) ResourcePlan {
	expansion := NewPromise(addr, func(self promise) (ResourcePlanData, tfdiags.Diagnostics) {
		ret := ResourcePlanData{
			Values:  map[addrs.InstanceKey]cty.Value{},
			PrevRun: prior.DeepCopy(),
			Refresh: prior.DeepCopy(),
			State:   prior.DeepCopy(),
		}

		evalCtx := scope.EvalContext(self)

		abstract, diags := tofuNodeAbstractResource(addr.Config(), config, scope)
		writeDiags := abstract.WriteResourceState(evalCtx, addr) // Oddly named, but this performs the expansion (for now)
		diags = diags.Append(writeDiags)

		// Some of the state manipulation here is doing pieces of states.Module.SetResourceInstanceCurrent
		for _, resAddr := range evalCtx.InstanceExpander().ExpandResource(addr) {
			key := resAddr.Resource.Key
			var priorInstance *states.ResourceInstance
			var priorProvider *addrs.AbsProviderConfig
			if prior != nil {
				priorInstance = prior.Instance(key)
				priorProvider = &prior.ProviderConfig
			}
			instance, instanceDiags := NewResourceInstancePlan(ctx, resAddr, config, priorInstance, priorProvider, self, scope)
			diags = diags.Append(instanceDiags)
			ret.Values[key] = instance.Value
			if instanceDiags.HasErrors() {
				continue
			}

			if instance.PrevRun != nil {
				// Update existing value
				ret.PrevRun.Instances[key] = instance.PrevRun
			}
			if instance.Refresh != nil {
				// Update existing value
				ret.Refresh.Instances[key] = instance.Refresh
			}
			if instance.State != nil {
				if ret.State == nil {
					ret.State = &states.Resource{
						Addr:           addr,
						Instances:      map[addrs.InstanceKey]*states.ResourceInstance{},
						ProviderConfig: instance.ProviderConfig,
					}

				}
				ret.State.Instances[key] = instance.State
			} else {
				delete(ret.State.Instances, key)
			}
			if instance.Change != nil {
				ret.Changes = append(ret.Changes, instance.Change)
			}
		}
		if len(ret.State.Instances) == 0 {
			ret.State = nil
		}

		return ret, diags
	})

	outputValue := NewPromise(&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		// expansion
		expanded, diags := expansion.Value(self)

		instances := expanded.Values

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

	return ResourcePlan{outputValue, expansion}
}

func NewResourceApply(ctx context.Context, addr addrs.AbsResource, config *configs.Resource, priorChanges *plans.Changes, priorState *states.State, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {
	type expanded struct {
		instances map[addrs.InstanceKey]*Promise[cty.Value]
		applys    Applys
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		abstract, diags := tofuNodeAbstractResource(addr.Config(), config, scope)
		writeDiags := abstract.WriteResourceState(evalCtx, addr) // Oddly named, but this performs the expansion (for now)
		diags = diags.Append(writeDiags)

		ret := expanded{
			instances: map[addrs.InstanceKey]*Promise[cty.Value]{},
		}
		for _, resAddr := range evalCtx.InstanceExpander().ExpandResource(addr) {
			promise, apply, newDiags := NewResourceInstanceApply(ctx, resAddr, config, priorChanges, priorState, scope)
			diags = diags.Append(newDiags)
			key := resAddr.Resource.Key
			ret.instances[key] = promise
			ret.applys = append(ret.applys, apply)

		}
		return ret, diags
	})

	outputValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		// expansion
		expanded, diags := expansion.Value(self)

		moduleInstances := make(map[addrs.InstanceKey]cty.Value)
		for key, mod := range expanded.instances {
			var modDiags tfdiags.Diagnostics
			moduleInstances[key], modDiags = mod.Value(self)
			diags = diags.Append(modDiags)
		}
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		// Lifted from tofu/evaluate.go

		switch {
		case config.Count != nil:
			length := -1
			for key := range moduleInstances {
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
			for key, instance := range moduleInstances {
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
			for key, mod := range moduleInstances {
				sk := key.(addrs.StringKey)
				instanceMap[string(sk)] = mod
			}
			return cty.ObjectVal(instanceMap), diags
		default:
			return moduleInstances[addrs.NoKey], diags
		}
	})

	apply := func(data ApplyData) tfdiags.Diagnostics {
		expanded, diags := expansion.Value(nil)
		diags = diags.Append(expanded.applys.Collect(data))
		return diags
	}

	return outputValue, apply, nil
}

var (
	// Until we have proper providers wired in to this system, this is a hack for testing unconfigured providers
	runningProviders = map[addrs.Provider]providers.Interface{}
	providersLock    sync.Mutex
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

type ResourceInstancePlan struct {
	Value          cty.Value
	PrevRun        *states.ResourceInstance
	Refresh        *states.ResourceInstance
	State          *states.ResourceInstance
	Change         *plans.ResourceInstanceChangeSrc
	ProviderConfig addrs.AbsProviderConfig
}

func NewResourceInstancePlan(ctx context.Context, addr addrs.AbsResourceInstance, config *configs.Resource, state *states.ResourceInstance, providerConfig *addrs.AbsProviderConfig, self promise, scope *Scope) (ResourceInstancePlan, tfdiags.Diagnostics) {
	var resource ResourceInstancePlan

	evalCtx := scope.EvalContext(self)

	// Create pre-expansion node for embedding
	abstract, diags := tofuNodeAbstractResource(addr.ConfigResource(), config, scope)
	if diags.HasErrors() {
		return resource, diags
	}

	abstractInstance := &tofu.NodeAbstractResourceInstance{
		NodeAbstractResource: abstract,

		Addr: addr,

		//TODO preDestroyRefresh bool

		// During import we may generate configuration for a resource, which needs
		// to be stored in the final change.
		//TODO generatedConfigHCL string
	}

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

	if state != nil {
		evalCtx.State().SetResourceInstanceCurrent(addr, state.Current, *providerConfig, state.ProviderKey)
		evalCtx.PrevRunState().SetResourceInstanceCurrent(addr, state.Current, *providerConfig, state.ProviderKey)
		evalCtx.RefreshState().SetResourceInstanceCurrent(addr, state.Current, *providerConfig, state.ProviderKey)
		// Hack
		node.AttachResourceState(&states.Resource{
			ProviderConfig: *providerConfig,
			Instances: map[addrs.InstanceKey]*states.ResourceInstance{
				addr.Resource.Key: state,
			},
		})
	}

	//TODO How does this work cross module? node.Dependencies []addrs.ConfigResource

	// Now that we have attached the config and state, we can resolve the requested provider
	// TransformProvider
	providedBy := node.ProvidedBy()
	resolvedProvider := tofu.ResolvedProvider{
		// TODO provider configs AbsProviderConfig for configured providers
		ProviderConfig: addrs.AbsProviderConfig{
			// Module:
			Provider: node.Provider(),
			//Alias:
		},
		KeyExpression: providedBy.KeyExpression,
		KeyModule:     providedBy.KeyModule,
		KeyResource:   providedBy.KeyResource,
		KeyExact:      providedBy.KeyExact,
	}
	node.SetProvider(resolvedProvider)
	resource.ProviderConfig = resolvedProvider.ProviderConfig

	// Wire in provider
	providerDiags := providersHack(ctx, evalCtx, scope, abstract.Provider())
	diags = diags.Append(providerDiags)
	if diags.HasErrors() {
		return resource, diags
	}

	// Execute
	diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(walkPlan)))

	resource.Change = evalCtx.Changes().GetResourceInstanceChange(addr, states.CurrentGen)
	resource.State = evalCtx.State().ResourceInstance(addr)
	resource.PrevRun = evalCtx.PrevRunState().ResourceInstance(addr)
	resource.Refresh = evalCtx.RefreshState().ResourceInstance(addr)

	if resource.State != nil {
		src := resource.State.GetGeneration(states.CurrentGen)
		if src != nil {
			ty := abstract.Schema.ImpliedType()
			val, valDiags := src.Decode(ty)
			diags = diags.Append(valDiags)
			resource.Value = val.Value
		}
	}

	return resource, diags
}

func NewResourceInstanceApply(ctx context.Context, addr addrs.AbsResourceInstance, config *configs.Resource, priorChanges *plans.Changes, priorState *states.State, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {

	type Resource struct {
		value cty.Value
		apply Apply
	}

	resource := NewPromise[Resource](addr, func(self promise) (Resource, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		// Create pre-expansion node for embedding
		abstract, diags := tofuNodeAbstractResource(addr.ConfigResource(), config, scope)
		if diags.HasErrors() {
			return Resource{}, diags
		}

		abstractInstance := &tofu.NodeAbstractResourceInstance{
			NodeAbstractResource: abstract,

			Addr: addr,

			//TODO preDestroyRefresh bool

			// During import we may generate configuration for a resource, which needs
			// to be stored in the final change.
			//TODO generatedConfigHCL string
		}

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

		if change := priorChanges.ResourceInstance(addr); change != nil {
			evalCtx.Changes().AppendResourceInstanceChange(change)
		}
		if state := priorState.ResourceInstance(addr); state != nil {
			priorResource := priorState.Resource(addr.ContainingResource())
			if priorResource != nil {
				evalCtx.State().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
				evalCtx.PrevRunState().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
				evalCtx.RefreshState().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
				node.AttachResourceState(priorResource)
			}
		}

		//TODO How does this work cross module? node.Dependencies []addrs.ConfigResource

		// Now that we have attached the config and state, we can resolve the requested provider
		// TransformProvider
		providedBy := node.ProvidedBy()
		resolvedProvider := tofu.ResolvedProvider{
			// TODO provider configs AbsProviderConfig for configured providers
			ProviderConfig: addrs.AbsProviderConfig{
				// Module:
				Provider: node.Provider(),
				//Alias:
			},
			KeyExpression: providedBy.KeyExpression,
			KeyModule:     providedBy.KeyModule,
			KeyResource:   providedBy.KeyResource,
			KeyExact:      providedBy.KeyExact,
		}
		node.SetProvider(resolvedProvider)

		// Wire in provider
		providerDiags := providersHack(ctx, evalCtx, scope, abstract.Provider())
		diags = diags.Append(providerDiags)
		if diags.HasErrors() {
			return Resource{}, diags
		}

		// Execute
		diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(walkApply)))

		var resource Resource

		stateSrc := evalCtx.State().ResourceInstanceObject(addr, states.CurrentGen)
		if stateSrc != nil {
			ty := abstract.Schema.ImpliedType()
			val, valDiags := stateSrc.Decode(ty)
			diags = diags.Append(valDiags)
			resource.value = val.Value
		}

		resource.apply = func(data ApplyData) tfdiags.Diagnostics {
			if stateSrc != nil {
				data.State.SetResourceInstanceCurrent(addr, stateSrc, node.ResolvedProvider.ProviderConfig, node.ResolvedProviderKey)
			}
			return nil
		}

		return resource, diags
	})

	resourceValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		resource, diags := resource.Value(self)
		return resource.value, diags
	})

	apply := func(data ApplyData) tfdiags.Diagnostics {
		resource, diags := resource.Value(nil)
		if resource.apply != nil {
			diags = diags.Append(resource.apply(data))
		}
		return diags
	}

	return resourceValue, apply, nil
}
