package engine

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/lang/evalchecks"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewModuleCall(ctx context.Context, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, priorChanges *plans.Changes, priorState *states.State, scope *Scope, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {

	type expanded struct {
		instances map[addrs.InstanceKey]*Promise[cty.Value]
		actions   Actions
	}

	expansion := NewPromise[expanded](addr, func(self promise) (expanded, tfdiags.Diagnostics) {
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
			return expanded{}, diags
		}

		// Legacy expander integration.  We should just be passing around RepetitionData instead.
		expander := scope.expander

		// Keep track resulting instance promises and their actions
		promises := map[addrs.InstanceKey]*Promise[cty.Value]{}
		var actions Actions

		addInstance := func(key addrs.InstanceKey) {
			input := map[addrs.InputVariable]VariableInput{}

			for _, v := range moduleConfig.Module.Variables {
				var expr hcl.Expression
				if attr := content.Attributes[v.Name]; attr != nil {
					expr = attr.Expr
				}
				input[addrs.InputVariable{Name: v.Name}] = VariableInput{
					expr:  expr,
					scope: scope,
				}
			}

			promise, action, newDiags := NewModule(ctx, addr.Instance(key), moduleConfig, priorChanges, priorState, scope, input, op)
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
			expander.SetModuleCount(addr.Module, addr.Call, count)

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
			expander.SetModuleForEach(addr.Module, addr.Call, forEachVals)

			for key := range forEachVals {
				addInstance(addrs.StringKey(key))
			}
		default:
			expander.SetModuleSingle(addr.Module, addr.Call)
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
