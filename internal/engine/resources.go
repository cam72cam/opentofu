package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
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
		return Resource{ValuePromise: NewPromise(Ident{addr, "(stub)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
			abstract, diags := tofuNodeAbstractResource(addr.Config(), config, scope)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			node := &tofu.NodeValidatableResource{&abstract}

			resolvedProvider := tofu.ResolvedProvider{
				// For validate, we can just depend on the unconfigured root instance
				ProviderConfig: addrs.AbsProviderConfig{Provider: abstract.Provider()},
			}
			abstract.SetProvider(resolvedProvider)

			_, execDiags := scope.LegacyExecute(ctx, self, node)
			diags = diags.Append(execDiags)

			// TODO this mirrors tofu/evaluate.go, but should be made a *lot* smarter as we know the output schema + if there are any count/for_each wrappers applied
			return cty.DynamicVal, diags
		})}
	}
	expansion := NewPromise(Ident{addr, "(expand)"}, func(self *Executor) (ResourceInstances, tfdiags.Diagnostics) {
		var diags tfdiags.Diagnostics
		instances := ResourceInstances{}

		// From NodeResourceAbstract
		// We'll record our expansion decision in the shared "expander" object
		// so that later operations (i.e. DynamicExpand and expression evaluation)
		// can refer to it. Since this node represents the abstract module, we need
		// to expand the module here to create all resources.
		expander := scope.expander

		switch {
		case config != nil && config.Count != nil:
			evalCtx := scope.EvalContext(self)
			count, cDiags := tofu.EvaluateCountExpression(config.Count, evalCtx, addr)
			diags = diags.Append(cDiags)
			if diags.HasErrors() {
				return instances, diags
			}

			// TODO state.SetResourceProvider(addr, n.ResolvedProvider.ProviderConfig)
			expander.SetResourceCount(addr.Module, addr.Resource, count)

		case config != nil && config.ForEach != nil:
			evalCtx := scope.EvalContext(self)
			forEach, feDiags := tofu.EvaluateForEachExpression(config.ForEach, evalCtx, addr)
			diags = diags.Append(feDiags)
			if diags.HasErrors() {
				return instances, diags
			}

			// This method takes care of all of the business logic of updating this
			// while ensuring that any existing instances are preserved, etc.
			// TODO state.SetResourceProvider(addr, n.ResolvedProvider.ProviderConfig)
			expander.SetResourceForEach(addr.Module, addr.Resource, forEach)

		default:
			// TODO state.SetResourceProvider(addr, n.ResolvedProvider.ProviderConfig)
			expander.SetResourceSingle(addr.Module, addr.Resource)
		}

		configAddr := addr.Resource.InModule(addr.Module.Module())

		// Some of the state manipulation here is doing pieces of states.Module.SetResourceInstanceCurrent
		for _, resAddr := range scope.expander.ExpandResource(addr) {
			resAddr := resAddr
			key := resAddr.Resource.Key

			if scope.op == walkPlan {
				if checkState := scope.Checks; checkState.ConfigHasChecks(configAddr) {
					scope.Checks.ReportCheckableObject(configAddr, resAddr)
				}
			}

			instances[key] = NewPromise(Ident{resAddr, "(instance)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
				return NewResourceInstance(ctx, resAddr, config, self, scope)
			})
		}
		return instances, diags
	})

	outputValue := NewPromise(Ident{addr, "(value)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		// expansion
		expanded, diags := expansion.Value(self)
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		instances := make(map[addrs.InstanceKey]cty.Value)
		for key, mod := range expanded {
			instances[key], diags = mod.Value(self)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
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
				return cty.EmptyTupleVal, nil
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
			return cty.TupleVal(vals), nil

		case config.ForEach != nil:
			instanceMap := make(map[string]cty.Value)
			for key, mod := range instances {
				sk := key.(addrs.StringKey)
				instanceMap[string(sk)] = mod
			}
			return cty.ObjectVal(instanceMap), nil
		default:
			return instances[addrs.NoKey], nil
		}
	})

	return Resource{outputValue, expansion}
}

func (m Resource) Expand(c *Manager, exec *Executor) {
	c.Add(m)

	if m.instances == nil {
		return
	}
	expanded, _ := m.instances.Value(exec)
	for _, res := range expanded {
		c.Add(res)
	}
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
	schema, schemaVersion, err := scope.Plugins().ResourceTypeSchema(abstract.Provider(), addr.Resource.Mode, addr.Resource.Type)
	if err != nil {
		diags = diags.Append(err)
	}
	abstract.AttachResourceSchema(schema, schemaVersion)

	// TODO
	// AttachProviderMetaConfigs(config.moduleConfig.Module.ProviderMetas)
	names := abstract.ProvisionedBy()
	for _, name := range names {
		schema, err := scope.Plugins().ProvisionerSchema(name)
		if err != nil {
			return abstract, diags.Append(fmt.Errorf("failed to read provisioner configuration schema for %q: %w", name, err))
		}
		if schema == nil {
			log.Printf("[ERROR] AttachSchemaTransformer: No schema available for provisioner %q on %q", name, abstract.Name())
			continue
		}
		log.Printf("[TRACE] AttachSchemaTransformer: attaching provisioner %q config schema to %s", name, abstract.Name())
		abstract.AttachProvisionerSchema(name, schema)
	}
	// AttachDataResourceDependsOn

	return abstract, diags

}

func NewResourceInstance(ctx context.Context, addr addrs.AbsResourceInstance, config *configs.Resource, self *Executor, scope *Scope) (cty.Value, tfdiags.Diagnostics) {
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

	if state := scope.PrevRun.Resource(addr.ContainingResource()); state != nil && state.Instance(addr.Resource.Key) != nil {
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

	// Execute
	if scope.op == walkPlan {
		node := &tofu.NodePlannableResourceInstance{
			NodeAbstractResourceInstance: abstractInstance,
			ForceCreateBeforeDestroy:     config.Managed.CreateBeforeDestroy,

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
		_, nodeDiags := scope.LegacyExecute(ctx, self, node)
		diags = diags.Append(nodeDiags)

	}
	if scope.op == walkApply {
		node := &tofu.NodeApplyableResourceInstance{
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
		_, nodeDiags := scope.LegacyExecute(ctx, self, node)
		diags = diags.Append(nodeDiags)
	}

	if src := scope.State.ResourceInstanceObject(addr, states.CurrentGen); src != nil {
		ty := abstract.Schema.ImpliedType()

		if src.Status == states.ObjectPlanned {
			// TODO make this match tofu/evaluate.go much more closely
			change := scope.ChangesSync.GetResourceInstanceChange(addr, states.CurrentGen)
			if change == nil {
				// If the object is in planned status then we should not get
				// here, since we should have found a pending value in the plan
				// above instead.
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Missing pending object in plan",
					Detail:   fmt.Sprintf("Instance %s is marked as having a change pending but that change is not recorded in the plan. This is a bug in OpenTofu; please report it.", addr),
					Subject:  &config.DeclRange,
				})
				return cty.NilVal, diags
			}

			val, err := change.After.Decode(ty)
			if err != nil {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid resource instance data in plan",
					Detail:   fmt.Sprintf("Instance %s data could not be decoded from the plan: %s.", addr, err),
					Subject:  &config.DeclRange,
				})
				return cty.NilVal, diags
			}

			afterMarks := change.AfterValMarks
			if abstract.Schema.ContainsSensitive() {
				// Now that we know that the schema contains sensitive marks,
				// Combine those marks together to ensure that the value is marked correctly but not double marked
				schemaMarks := abstract.Schema.ValueMarks(val, nil)
				afterMarks = combinePathValueMarks(afterMarks, schemaMarks)
			}

			return val.MarkWithPaths(afterMarks), diags
		}
		val, valDiags := src.Decode(ty)
		diags = diags.Append(valDiags)
		return val.Value, diags
	}

	return cty.NilVal, diags
}

// From tofu

func copyPathValueMarks(marks cty.PathValueMarks) cty.PathValueMarks {
	newMarks := make(cty.ValueMarks, len(marks.Marks))
	result := cty.PathValueMarks{Path: marks.Path}
	for k, v := range marks.Marks {
		newMarks[k] = v
	}
	result.Marks = newMarks
	return result
}

// combinePathValueMarks will combine the marks from two sets of marks with paths, ensuring that we don't duplicate marks
// for the same path, but instead combine the marks for the same path
// This ensures that we don't lose user marks when combining 2 different sets of marks for the same path
func combinePathValueMarks(marks []cty.PathValueMarks, other []cty.PathValueMarks) []cty.PathValueMarks {
	// skip some work if we don't have any marks in either of the lists
	if len(marks) == 0 {
		return other
	}
	if len(other) == 0 {
		return marks
	}

	combined := make([]cty.PathValueMarks, 0, len(marks))
	// construct the initial set of marks
	combined = append(combined, marks...)

	// check if we've already inserted this by looping over and calling .Equals().
	// This isn't so nice but there is no nice comparison for cty.PathValueMarks
	// so we have to do it this way
	for _, mark := range other {
		exists := false
		for i, existing := range combined {
			if mark.Path.Equals(existing.Path) {
				// if we found a matching path, we should combine the marks and update the existing item
				dupe := copyPathValueMarks(existing)
				for k, v := range mark.Marks {
					dupe.Marks[k] = v
				}
				combined[i] = dupe
				exists = true
				break
			}
		}
		// Otherwise we haven't seen this path before, so we should add it to the list
		// no merging required
		if !exists {
			combined = append(combined, mark)
		}
	}

	return combined
}
