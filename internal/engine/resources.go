package engine

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/configs/hcl2shim"
	"github.com/opentofu/opentofu/internal/lang/evalchecks"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/providers"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewResource(ctx context.Context, addr addrs.AbsResource, config *configs.Resource, priorChanges *plans.Changes, priorState *states.State, scope *Scope, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {
	// TODO this is 99% copy pasted from NewModuleCall and could easily be refactored out.
	type expanded struct {
		instances map[addrs.InstanceKey]*Promise[cty.Value]
		actions   Actions
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
		// Legacy expander integration.  We should just be passing around RepetitionData instead.
		expander := scope.expander

		// Keep track resulting instance promises and their actions
		promises := map[addrs.InstanceKey]*Promise[cty.Value]{}
		var actions Actions
		var diags tfdiags.Diagnostics

		addInstance := func(key addrs.InstanceKey) {
			promise, action, newDiags := NewResourceInstance(ctx, addr.Instance(key), config, priorChanges, priorState, scope, op)
			promises[key] = promise
			actions = append(actions, action)
			diags = diags.Append(newDiags)
		}

		switch {
		case config.Count != nil:
			count, countDiags := evalchecks.EvaluateCountExpression(
				config.Count,
				func(expr hcl.Expression) (cty.Value, tfdiags.Diagnostics) {
					return scope.EvalContext(self).EvaluateExpr(expr, cty.Number, nil)
				},
				nil,
			)

			diags = diags.Append(countDiags)
			expander.SetResourceCount(addr.Module, addr.Resource, count)

			for index := range count {
				addInstance(addrs.IntKey(index))
			}
		case config.ForEach != nil:
			// TODO make sure we are using the right call here
			forEachVals, forEachDiags := evalchecks.EvaluateForEachExpression(
				config.ForEach,
				func(refs []*addrs.Reference) (*hcl.EvalContext, tfdiags.Diagnostics) {
					scope := scope.EvalContext(self).EvaluationScope(nil, nil, tofu.InstanceKeyEvalData{})
					return scope.EvalContext(refs)
				}, nil)

			diags = diags.Append(forEachDiags)
			expander.SetResourceForEach(addr.Module, addr.Resource, forEachVals)

			for key := range forEachVals {
				addInstance(addrs.StringKey(key))
			}
		default:
			expander.SetResourceSingle(addr.Module, addr.Resource)
			addInstance(addrs.NoKey)
		}

		return expanded{promises, actions}, diags
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

	action := func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
		expanded, diags := expansion.Value(nil)
		diags = diags.Append(expanded.actions.Parallel(change, state))
		return diags
	}

	return outputValue, action, nil
}

func NewResourceInstance(ctx context.Context, addr addrs.AbsResourceInstance, config *configs.Resource, priorChanges *plans.Changes, priorState *states.State, scope *Scope, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {

	type Resource struct {
		value  cty.Value
		action Action
	}

	resource := NewPromise[Resource](addr, func(self promise) (Resource, tfdiags.Diagnostics) {
		var diags tfdiags.Diagnostics

		evalCtx := scope.EvalContext(self)

		abstract := &tofu.NodeAbstractResourceInstance{
			NodeAbstractResource: tofu.NodeAbstractResource{
				Addr: addr.ConfigResource(),

				// The fields below will be automatically set using the Attach
				// interfaces if you're running those transforms, but also be explicitly
				// set if you already have that information.

				// ProviderMetas is the provider_meta configs for the module this resource belongs to
				// TODO ProviderMetas map[addrs.Provider]*configs.ProviderMeta

				// TODO ProvisionerSchemas map[string]*configschema.Block

				// Set from GraphNodeTargetable
				// TODO Targets []addrs.Targetable

				// Set from GraphNodeTargetable
				// TODO Excludes []addrs.Targetable

				// Set from AttachDataResourceDependsOn
				// TODO dependsOn      []addrs.ConfigResource
				// TODO forceDependsOn bool

				// The address of the provider this resource will use
				// TODO ResolvedProvider ResolvedProvider

				// storedProviderConfig is the provider address retrieved from the
				// state. This is defined here for access within the ProvidedBy method, but
				// will be set from the embedding instance type when the state is attached.
				// TODO storedProviderConfig ResolvedProvider

				// This resource may expand into instances which need to be imported.
				// TODO importTargets []*ImportTarget

				// generateConfigPath tells this node which file to write generated config
				// into. If empty, then config should not be generated.
				// TODO generateConfigPath string

				// removedBlockProvisioners holds any possibly existing configs.Provisioner configs that could be defined by using
				// removed.provisioner configuration. If the field "Config.Managed.Provisioners" is having no provisioners, then
				// these provisioners should be used instead.
				// TODO removedBlockProvisioners []*configs.Provisioner
			},
			Addr: addr,

			// These are set via the AttachState method.
			//TODO instanceState *states.ResourceInstance

			//TODO Dependencies []addrs.ConfigResource

			//TODO preDestroyRefresh bool

			// During import we may generate configuration for a resource, which needs
			// to be stored in the final change.
			//TODO generatedConfigHCL string

			ResolvedProviderKey: addrs.NoKey, // TODO
		}

		// Make sure previous change is recorded
		if priorChanges != nil {
			if change := priorChanges.ResourceInstance(addr); change != nil {
				evalCtx.Changes().AppendResourceInstanceChange(change)
			}
		}
		if priorState != nil {
			if state := priorState.ResourceInstance(addr); state != nil {
				priorResource := priorState.Resource(addr.ContainingResource())
				if priorResource != nil {
					evalCtx.State().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
					evalCtx.PrevRunState().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
					evalCtx.RefreshState().SetResourceInstanceCurrent(addr, state.Current, priorResource.ProviderConfig, state.ProviderKey)
					abstract.AttachResourceState(priorResource)
				}
			}
		}

		// AttachResourceConfigTransformer
		abstract.AttachResourceConfig(config)
		//if moduleConfig.Module.ProviderMetas != nil {
		//	abstract.AttachProviderMetaConfigs(config.moduleConfig.Module.ProviderMetas)
		//}

		// AttachSchemaTransformer
		schema, schemaVersion, err := scope.Plugins.ResourceTypeSchema(abstract.Provider(), addr.Resource.Resource.Mode, addr.Resource.Resource.Type)
		if err != nil {
			return Resource{}, diags.Append(err)
		}
		abstract.AttachResourceSchema(schema, schemaVersion)

		// Now that we have attached the config and state, we can resolve the requested provider
		// TransformProvider
		providedBy := abstract.ProvidedBy()
		resolvedProvider := tofu.ResolvedProvider{
			// TODO provider configs AbsProviderConfig for configured providers
			ProviderConfig: addrs.AbsProviderConfig{
				// Module:
				Provider: abstract.Provider(),
				//Alias:
			},
			KeyExpression: providedBy.KeyExpression,
			KeyModule:     providedBy.KeyModule,
			KeyResource:   providedBy.KeyResource,
			KeyExact:      providedBy.KeyExact,
		}
		abstract.SetProvider(resolvedProvider)

		// Wire in provider
		mock := evalCtx.(*tofu.MockEvalContext)
		mock.ProviderSchemaSchema, err = scope.Plugins.ProviderSchema(abstract.Provider())
		if err != nil {
			return Resource{}, diags.Append(err)
		}
		// TODO use actually resolved provider
		mock.ProviderProvider, err = scope.Plugins.NewProviderInstance(abstract.Provider())
		if err != nil {
			return Resource{}, diags.Append(err)
		}
		// TODO HACK actually configure resolved provider
		{

			configSchema, _ := scope.Plugins.ProviderConfigSchema(abstract.Provider())
			configBody := hcl2shim.SynthBody(abstract.Provider().String(), make(map[string]cty.Value))
			configVal, _, evalDiags := evalCtx.EvaluateBlock(configBody, configSchema, nil, tofu.EvalDataForNoInstanceKey)
			if evalDiags.HasErrors() {
				return Resource{}, diags.Append(evalDiags)
			}

			cpr := mock.ProviderProvider.ConfigureProvider(ctx, providers.ConfigureProviderRequest{
				TerraformVersion: "1.10.0",
				Config:           configVal,
			})
			diags = diags.Append(cpr.Diagnostics)
			if diags.HasErrors() {
				return Resource{}, diags
			}
		}

		resource := Resource{}
		if op == walkPlan {
			node := tofu.NodePlannableResourceInstance{
				NodeAbstractResourceInstance: abstract,
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
			diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(op)))
		}
		if op == walkApply {
			node := tofu.NodeApplyableResourceInstance{
				NodeAbstractResourceInstance: abstract,

				// TODO graphNodeDeposer // implementation of GraphNodeDeposerConfig

				// If this node is forced to be CreateBeforeDestroy, we need to record that
				// in the state to.
				// TODO ForceCreateBeforeDestroy bool

				// forceReplace are resource instance addresses where the user wants to
				// force generating a replace action. This set isn't pre-filtered, so
				// it might contain addresses that have nothing to do with the resource
				// that this node represents, which the node itself must therefore ignore.
				// TODO forceReplace []addrs.AbsResourceInstance
			}
			diags = diags.Append(node.Execute(ctx, evalCtx, tofu.WalkOperation(op)))
		}

		changeSrc := evalCtx.Changes().GetResourceInstanceChange(addr, states.CurrentGen)
		stateSrc := evalCtx.State().ResourceInstanceObject(addr, states.CurrentGen)
		if stateSrc != nil {
			ty := schema.ImpliedType()
			val, valDiags := stateSrc.Decode(ty)
			diags = diags.Append(valDiags)
			resource.value = val.Value
		}

		resource.action = func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
			if stateSrc != nil {
				state.SetResourceInstanceCurrent(addr, stateSrc, abstract.ResolvedProvider.ProviderConfig, abstract.ResolvedProviderKey)
			}
			if changeSrc != nil {
				change.AppendResourceInstanceChange(changeSrc)
			}
			return nil
		}

		return resource, diags
	})

	// TODO better promise refinement
	resourceValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		resource, diags := resource.Value(self)
		return resource.value, diags
	})

	action := func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
		resource, diags := resource.Value(nil)
		if resource.action != nil {
			diags = diags.Append(resource.action(change, state))
		}
		return diags
	}

	return resourceValue, action, nil
}
