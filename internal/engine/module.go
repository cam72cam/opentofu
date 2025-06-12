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

type ValuePromise interface {
	Value(promise) (cty.Value, tfdiags.Diagnostics)
}

type ModuleData[Variable, Local, Resource, Call, Output ValuePromise] struct {
	Addr      addrs.ModuleInstance
	Variables map[addrs.InputVariable]Variable
	Locals    map[addrs.LocalValue]Local
	Resources map[addrs.Resource]Resource
	Calls     map[addrs.ModuleCall]Call
	Outputs   map[addrs.OutputValue]Output
}

func NewModuleData[Variable, Local, Resource, Call, Output ValuePromise](addr addrs.ModuleInstance) ModuleData[Variable, Local, Resource, Call, Output] {
	return ModuleData[Variable, Local, Resource, Call, Output]{
		Addr:      addr,
		Variables: map[addrs.InputVariable]Variable{},
		Locals:    map[addrs.LocalValue]Local{},
		Resources: map[addrs.Resource]Resource{},
		Calls:     map[addrs.ModuleCall]Call{},
		Outputs:   map[addrs.OutputValue]Output{},
	}
}

type ModuleValidate struct {
	*Promise[cty.Value]
	ModuleData[ValuePromise, ValuePromise, ValuePromise, ModuleCallValidate, ValuePromise]
}

func NewModuleValidate(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, parentScope *Scope) ModuleValidate {
	data := NewModuleData[ValuePromise, ValuePromise, ValuePromise, ModuleCallValidate, ValuePromise](addr)
	scope := NewScopeAlt(addr, parentScope, data)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		data.Variables[varAddr] = NewVariableValidate(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		data.Locals[localAddr] = NewLocalValidate(ctx, localAddr.Absolute(addr), local, scope)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResourceValidate(ctx, resAddr.Absolute(addr), resource, scope)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResourceValidate(ctx, resAddr.Absolute(addr), resource, scope)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		data.Calls[callAddr] = NewModuleCallValidate(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], scope)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		data.Outputs[outputAddr] = NewOutputValidate(ctx, outputAddr.Absolute(addr), output, scope)
	}

	return ModuleValidate{
		NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
			obj := map[string]cty.Value{}
			var diags tfdiags.Diagnostics
			for name := range config.Module.Outputs {
				outVal, outDiags := data.Outputs[addrs.OutputValue{Name: name}].Value(self)
				obj[name] = outVal
				diags = diags.Append(outDiags)
			}
			return cty.ObjectVal(obj), diags
		}),
		data,
	}
}

func validateCollection[T comparable](m map[T]ValuePromise) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	for _, p := range m {
		_, newDiags := p.Value(nil)
		diags = diags.Append(newDiags)
	}
	return diags
}
func (m *ModuleValidate) Collect() tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	diags = diags.Append(validateCollection(m.Variables))
	diags = diags.Append(validateCollection(m.Locals))
	diags = diags.Append(validateCollection(m.Resources))
	// Follow Expansion
	for _, call := range m.Calls {
		child, newDiags := call.instance.Value(nil)
		diags = diags.Append(newDiags)
		diags = diags.Append(child.Collect())
	}
	diags = diags.Append(validateCollection(m.Outputs))

	return diags
}

type ModulePlan struct {
	*Promise[cty.Value]
	ModuleData[VariablePlan, LocalPlan, ResourcePlan, ModuleCallPlan, OutputPlan]
}

func NewModulePlan(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, priorState *states.State, parentScope *Scope) ModulePlan {
	data := NewModuleData[VariablePlan, LocalPlan, ResourcePlan, ModuleCallPlan, OutputPlan](addr)
	scope := NewScopeAlt(addr, parentScope, data)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		data.Variables[varAddr] = NewVariablePlan(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		data.Locals[localAddr] = NewLocalPlan(ctx, localAddr.Absolute(addr), local, scope)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResourcePlan(ctx, resAddr.Absolute(addr), resource, priorState.Resource(resAddr.Absolute(addr)), scope)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResourcePlan(ctx, resAddr.Absolute(addr), resource, priorState.Resource(resAddr.Absolute(addr)), scope)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		data.Calls[callAddr] = NewModuleCallPlan(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], priorState, scope)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		data.Outputs[outputAddr] = NewOutputPlan(ctx, outputAddr.Absolute(addr), output, priorState.OutputValue(outputAddr.Absolute(addr)), scope)
	}

	outputValue := NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		obj := map[string]cty.Value{}
		var diags tfdiags.Diagnostics
		for name := range config.Module.Outputs {
			outVal, outDiags := data.Outputs[addrs.OutputValue{Name: name}].Value(self)
			obj[name] = outVal
			diags = diags.Append(outDiags)
		}
		return cty.ObjectVal(obj), diags
	})

	return ModulePlan{outputValue, data}
}

type ModulePlanData struct {
	PrevRun   []*states.Module
	Refresh   []*states.Module
	State     []*states.Module
	Resources []*plans.ResourceInstanceChangeSrc
	Outputs   []*plans.OutputChangeSrc
}

func (m ModulePlan) PlanData() (ModulePlanData, tfdiags.Diagnostics) {
	var data ModulePlanData
	var diags tfdiags.Diagnostics

	prevRun := states.NewModule(m.Addr)
	refresh := states.NewModule(m.Addr)
	state := states.NewModule(m.Addr)

	// Collect resource results
	for addr, resource := range m.Resources {
		res, planDiags := resource.Data.Value(nil)
		diags = diags.Append(planDiags)
		if res.Changes != nil {
			data.Resources = append(data.Resources, res.Changes...)
		}
		if res.PrevRun != nil {
			prevRun.Resources[addr.String()] = res.PrevRun
		}
		if res.Refresh != nil {
			refresh.Resources[addr.String()] = res.Refresh
		}
		if res.State != nil {
			state.Resources[addr.String()] = res.State
		}
	}

	// Collect Call results
	for _, call := range m.Calls {
		instances, instanceDiags := call.instances.Value(nil)
		diags = diags.Append(instanceDiags)
		for _, instance := range instances {
			modData, modDiags := instance.PlanData()
			diags = diags.Append(modDiags)

			// Merge changes
			data.PrevRun = append(data.PrevRun, modData.PrevRun...)
			data.Refresh = append(data.Refresh, modData.Refresh...)
			data.State = append(data.State, modData.State...)
			data.Resources = append(data.Resources, modData.Resources...)
			data.Outputs = append(data.Outputs, modData.Outputs...)
		}
	}

	// Collect Output results
	for addr, output := range m.Outputs {
		outData, outDiags := output.promise.Value(nil)
		diags = diags.Append(outDiags)
		if outData.change != nil {
			data.Outputs = append(data.Outputs, outData.change)
		}
		if outData.prevRun != nil {
			prevRun.OutputValues[addr.Name] = outData.prevRun
		}
		if outData.refresh != nil {
			refresh.OutputValues[addr.Name] = outData.refresh
		}
		if outData.state != nil {
			state.OutputValues[addr.Name] = outData.state
		}
	}

	// TODO don't set if not populated?
	data.PrevRun = append(data.PrevRun, prevRun)
	data.Refresh = append(data.Refresh, refresh)
	data.State = append(data.State, state)

	return data, diags
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
