package engine

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func getModuleCallInputExpressions(config *configs.ModuleCall, moduleConfig *configs.Config) (map[string]hcl.Expression, tfdiags.Diagnostics) {
	// Lifted from transform_module_variable

	// We need to construct a schema for the expected call arguments based on
	// the configured variables in our config, which we can then use to
	// decode the content of the call block.
	schema := &hcl.BodySchema{}
	for _, v := range moduleConfig.Module.Variables {
		schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{
			Name:     v.Name,
			Required: v.Default == cty.NilVal,
		})
	}

	content, contentDiags := config.Config.Content(schema)
	diags := tfdiags.Diagnostics{}.Append(contentDiags)
	if diags.HasErrors() {
		// Validation code elsewhere should deal with any errors before we
		// get in here, but we'll report them out here just in case, to
		// avoid crashes.
		return nil, diags
	}

	exprs := map[string]hcl.Expression{}

	for name, attr := range content.Attributes {
		exprs[name] = attr.Expr
	}

	return exprs, diags

}

func NewModuleCallValidate(ctx context.Context, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, scope *Scope) (*Promise[cty.Value], Validate, tfdiags.Diagnostics) {
	// Validate only ever does a single expansion

	type expanded struct {
		instance *Promise[cty.Value]
		validate Validate
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		node := tofu.NodeValidateModule{tofu.NodeExpandModule{
			Addr:       append(addr.Module.Module(), config.Name),
			Config:     moduleConfig.Module,
			ModuleCall: config,
		}}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkValidate))

		exprs, exprDiags := getModuleCallInputExpressions(config, moduleConfig)
		diags = diags.Append(exprDiags)

		input := VariableInputs{}
		for _, v := range moduleConfig.Module.Variables {
			input[addrs.InputVariable{Name: v.Name}] = VariableInput{
				expr:  exprs[v.Name], // TODO this differs from existing tofu logic
				scope: scope,
			}
		}
		promise, validate, newDiags := NewModuleValidate(ctx, addr.Instance(addrs.NoKey), moduleConfig, input, scope)
		return expanded{promise, validate}, diags.Append(newDiags)
	})

	outputValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		expanded, diags := expansion.Value(self)
		out, outDiags := expanded.instance.Value(self)
		diags = diags.Append(outDiags)

		// FROM: tofu/evaluate.go
		// While we know the type here and it would be nice to validate whether
		// indexes are valid or not, because tuples and objects have fixed
		// numbers of elements we can't simply return an unknown value of the
		// same type since we have not expanded any instances during
		// validation.
		//
		// In order to validate the expression a little precisely, we'll create
		// an unknown map or list here to get more type information.
		ty := out.Type()
		switch {
		case config.Count != nil:
			return cty.UnknownVal(cty.List(ty)), diags
		case config.ForEach != nil:
			return cty.UnknownVal(cty.Map(ty)), diags
		default:
			return cty.UnknownVal(ty), diags
		}
	})

	validate := func() tfdiags.Diagnostics {
		expanded, diags := expansion.Value(nil)
		return diags.Append(expanded.validate())
	}

	return outputValue, validate, nil
}

func NewModuleCallPlan(ctx context.Context, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, priorState *states.State, scope *Scope) (*Promise[cty.Value], Plan, tfdiags.Diagnostics) {

	type expanded struct {
		instances map[addrs.InstanceKey]*Promise[cty.Value]
		plans     Plans
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		node := tofu.NodeExpandModule{
			Addr:       append(addr.Module.Module(), config.Name),
			Config:     moduleConfig.Module,
			ModuleCall: config,
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkPlan))

		exprs, exprDiags := getModuleCallInputExpressions(config, moduleConfig)
		diags = diags.Append(exprDiags)

		ret := expanded{
			instances: map[addrs.InstanceKey]*Promise[cty.Value]{},
		}
		for _, modAddr := range evalCtx.InstanceExpander().ExpandAbsModuleCall(addr) {
			input := VariableInputs{}
			for _, v := range moduleConfig.Module.Variables {
				input[addrs.InputVariable{Name: v.Name}] = VariableInput{
					expr:  exprs[v.Name],
					scope: scope,
				}
			}
			promise, plan, newDiags := NewModulePlan(ctx, modAddr, moduleConfig, input, priorState, scope)
			diags = diags.Append(newDiags)
			key := modAddr[len(modAddr)-1].InstanceKey
			ret.instances[key] = promise
			ret.plans = append(ret.plans, plan)

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

	plan := func(data PlanData) tfdiags.Diagnostics {
		expanded, diags := expansion.Value(nil)
		diags = diags.Append(expanded.plans.Collect(data))
		return diags
	}

	return outputValue, plan, nil
}

func NewModuleCallApply(ctx context.Context, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, priorChanges *plans.Changes, priorState *states.State, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {

	type expanded struct {
		instances map[addrs.InstanceKey]*Promise[cty.Value]
		applys    Applys
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		node := tofu.NodeExpandModule{
			Addr:       append(addr.Module.Module(), config.Name),
			Config:     moduleConfig.Module,
			ModuleCall: config,
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkApply))

		exprs, exprDiags := getModuleCallInputExpressions(config, moduleConfig)
		diags = diags.Append(exprDiags)

		ret := expanded{
			instances: map[addrs.InstanceKey]*Promise[cty.Value]{},
		}
		for _, modAddr := range evalCtx.InstanceExpander().ExpandAbsModuleCall(addr) {
			input := VariableInputs{}
			for _, v := range moduleConfig.Module.Variables {
				input[addrs.InputVariable{Name: v.Name}] = VariableInput{
					expr:  exprs[v.Name],
					scope: scope,
				}
			}
			promise, apply, newDiags := NewModuleApply(ctx, modAddr, moduleConfig, input, priorChanges, priorState, scope)
			diags = diags.Append(newDiags)
			key := modAddr[len(modAddr)-1].InstanceKey
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
