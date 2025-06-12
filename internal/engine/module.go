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

func NewModuleValidate(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, parentScope *Scope) (*Promise[cty.Value], Validate, tfdiags.Diagnostics) {
	var validates Validates
	var diags tfdiags.Diagnostics

	scope := NewScope(addr, parentScope)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		promise, validate, newDiags := NewVariableValidate(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope)
		scope.variables[varAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		promise, validate, newDiags := NewLocalValidate(ctx, localAddr.Absolute(addr), local, scope)
		scope.locals[localAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, validate, newDiags := NewResourceValidate(ctx, resAddr.Absolute(addr), resource, scope)
		scope.resources[resAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, validate, newDiags := NewResourceValidate(ctx, resAddr.Absolute(addr), resource, scope)
		scope.resources[resAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		promise, validate, newDiags := NewModuleCallValidate(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], scope)
		scope.calls[callAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		promise, validate, newDiags := NewOutputValidate(ctx, outputAddr.Absolute(addr), output, scope)
		scope.outputs[outputAddr] = promise
		validates = append(validates, validate)
		diags = diags.Append(newDiags)
	}

	return NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		obj := map[string]cty.Value{}
		var diags tfdiags.Diagnostics
		for name := range config.Module.Outputs {
			outVal, outDiags := scope.outputs[addrs.OutputValue{Name: name}].Value(self)
			obj[name] = outVal
			diags = diags.Append(outDiags)
		}
		return cty.ObjectVal(obj), diags
	}), validates.Collect, diags
}

func NewModulePlan(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, priorState *states.State, parentScope *Scope) (*Promise[cty.Value], Plan, tfdiags.Diagnostics) {
	var plans Plans
	var diags tfdiags.Diagnostics

	scope := NewScope(addr, parentScope)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		promise, plan, newDiags := NewVariablePlan(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope)
		scope.variables[varAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		promise, plan, newDiags := NewLocalPlan(ctx, localAddr.Absolute(addr), local, scope)
		scope.locals[localAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, plan, newDiags := NewResourcePlan(ctx, resAddr.Absolute(addr), resource, priorState, scope)
		scope.resources[resAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, plan, newDiags := NewResourcePlan(ctx, resAddr.Absolute(addr), resource, priorState, scope)
		scope.resources[resAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		promise, plan, newDiags := NewModuleCallPlan(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], priorState, scope)
		scope.calls[callAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		promise, plan, newDiags := NewOutputPlan(ctx, outputAddr.Absolute(addr), output, priorState, scope)
		scope.outputs[outputAddr] = promise
		plans = append(plans, plan)
		diags = diags.Append(newDiags)
	}

	return NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		obj := map[string]cty.Value{}
		var diags tfdiags.Diagnostics
		for name := range config.Module.Outputs {
			outVal, outDiags := scope.outputs[addrs.OutputValue{Name: name}].Value(self)
			obj[name] = outVal
			diags = diags.Append(outDiags)
		}
		return cty.ObjectVal(obj), diags
	}), plans.Collect, diags
}

func NewModuleApply(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, priorChanges *plans.Changes, priorState *states.State, parentScope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {
	var applys Applys
	var diags tfdiags.Diagnostics

	scope := NewScope(addr, parentScope)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		promise, apply, newDiags := NewVariableApply(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope)
		scope.variables[varAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		promise, apply, newDiags := NewLocalApply(ctx, localAddr.Absolute(addr), local, scope)
		scope.locals[localAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, apply, newDiags := NewResourceApply(ctx, resAddr.Absolute(addr), resource, priorChanges, priorState, scope)
		scope.resources[resAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		promise, apply, newDiags := NewResourceApply(ctx, resAddr.Absolute(addr), resource, priorChanges, priorState, scope)
		scope.resources[resAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		promise, apply, newDiags := NewModuleCallApply(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], priorChanges, priorState, scope)
		scope.calls[callAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		promise, apply, newDiags := NewOutputApply(ctx, outputAddr.Absolute(addr), output, priorChanges, priorState, scope)
		scope.outputs[outputAddr] = promise
		applys = append(applys, apply)
		diags = diags.Append(newDiags)
	}

	return NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		obj := map[string]cty.Value{}
		var diags tfdiags.Diagnostics
		for name := range config.Module.Outputs {
			outVal, outDiags := scope.outputs[addrs.OutputValue{Name: name}].Value(self)
			obj[name] = outVal
			diags = diags.Append(outDiags)
		}
		return cty.ObjectVal(obj), diags
	}), applys.Collect, diags
}
