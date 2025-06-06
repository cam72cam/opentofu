package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

func NewModule(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, priorChanges *plans.Changes, priorState *states.State, parentScope *Scope, input map[addrs.InputVariable]VariableInput, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {
	var actions Actions
	var diags tfdiags.Diagnostics

	var outputValue *Promise[cty.Value]

	if config != nil {
		scope := NewScope(addr, op, parentScope)

		for _, variable := range config.Module.Variables {
			variable := variable
			varAddr := addrs.InputVariable{Name: variable.Name}

			promise, action, newDiags := NewVariable(ctx, varAddr.Absolute(addr), variable, input[varAddr], scope, op)

			scope.variables[varAddr] = promise
			actions = append(actions, action)
			diags = diags.Append(newDiags)
		}
		for _, local := range config.Module.Locals {
			local := local

			localAddr := addrs.LocalValue{Name: local.Name}
			promise, action, newDiags := NewLocal(ctx, localAddr.Absolute(addr), local, scope, op)

			scope.locals[localAddr] = promise
			actions = append(actions, action)
			diags = diags.Append(newDiags)
		}
		for _, resource := range config.Module.ManagedResources {
			resource := resource

			resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
			scope.resources[resAddr] = nil // TODO
		}
		// TODO DataResources
		for _, call := range config.Module.ModuleCalls {
			call := call

			callAddr := addrs.ModuleCall{Name: call.Name}

			promise, action, newDiags := NewModuleCall(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], priorChanges, priorState, scope, op)

			scope.calls[callAddr] = promise
			actions = append(actions, action)
			diags = diags.Append(newDiags)
		}
		for _, output := range config.Module.Outputs {
			output := output

			outputAddr := addrs.OutputValue{Name: output.Name}
			promise, action, newDiags := NewOutput(ctx, outputAddr.Absolute(addr), output, priorChanges, priorState, scope, op)

			scope.outputs[outputAddr] = promise
			actions = append(actions, action)
			diags = diags.Append(newDiags)
		}

		outputValue = NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
			obj := map[string]cty.Value{}
			var diags tfdiags.Diagnostics
			for name := range config.Module.Outputs {
				outVal, outDiags := scope.outputs[addrs.OutputValue{Name: name}].Value(outputValue)
				diags = diags.Append(outDiags)

				obj[name] = outVal
			}
			return cty.ObjectVal(obj), diags
		})

	}
	//TODO  data.InputState / data.InputChanges

	// We have fully built all things we are responsible for and can now return:
	// - Outputs that can be queried at will
	// - Translation to the legacy change and state formats

	return outputValue, actions.Parallel, nil
}
