package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/instances"
)

/*

Tree of ModuleCall -> Module -*> ModuleCall -> Module etc...
With each ModuleCall serving as the expander tracker

Information Needed?
* AllKeys => AllInstances
* ActiveKeys => InstanceData


*/

type Status int

const (
	StatusUnknown   = 0
	StatusPending   = 1
	StatusAvailable = 2
)

type Root struct {
	ModuleCall     *ModuleCall
	ModuleInstance *ModuleInstance
}

type ModuleCall struct {
	Addr   addrs.Module
	Config *configs.ModuleCall
	Module *Module

	Status     Status
	Dependents []struct{}
}

type Module struct {
	Addr   addrs.Module
	Call   *ModuleCall
	Config *configs.Config

	Variables map[addrs.InputVariable]*Variable
	Calls     map[addrs.ModuleCall]*ModuleCall
	Resources map[addrs.Resource]*Resource
	Outputs   map[addrs.OutputValue]*Output
}

type ModuleCallInstances struct {
	Caller     *ModuleInstance
	ModuleCall *ModuleCall
	Instances  map[addrs.InstanceKey]*ModuleInstance
}

type ModuleInstance struct {
	Addr   addrs.ModuleInstance
	Module *Module

	Call           *ModuleCallInstances
	RepetitionData instances.RepetitionData

	Variables       map[addrs.InputVariable]*VariableInstance
	Calls           map[addrs.ModuleCall]*ModuleCallInstances
	Resources       map[addrs.Resource]ResourceInstances
	OutputInstances map[addrs.OutputValue]*OutputInstance
}

func NewModuleCall(addr addrs.Module, call *configs.ModuleCall, config *configs.Config) *ModuleCall {
	fmt.Printf("Creating module call %s\n", addr)
	mc := &ModuleCall{
		Addr:   addr,
		Config: call,
	}
	mc.Module = NewModule(addr, mc, config)
	return mc
}

func NewModule(addr addrs.Module, call *ModuleCall, config *configs.Config) *Module {
	fmt.Printf("Creating module %s\n", addr)
	m := &Module{
		Addr:   addr,
		Call:   call,
		Config: config,

		Variables: map[addrs.InputVariable]*Variable{},
		Resources: map[addrs.Resource]*Resource{},
		Calls:     map[addrs.ModuleCall]*ModuleCall{},
		Outputs:   map[addrs.OutputValue]*Output{},
	}

	if config == nil {
		return m
	}

	for name, call := range config.Module.ModuleCalls {
		m.Calls[addrs.ModuleCall{Name: name}] = NewModuleCall(addr.Child(name), call, config.Children[name])
	}

	return m
}

func NewModuleCallInstances(caller *ModuleInstance, call *ModuleCall) *ModuleCallInstances {
	return &ModuleCallInstances{
		Caller:     caller,
		ModuleCall: call,
		Instances:  map[addrs.InstanceKey]*ModuleInstance{},
	}
}

func NewModuleInstance(addr addrs.ModuleInstance, module *Module, call *ModuleCallInstances) *ModuleInstance {
	fmt.Printf("Creating module instance %s\n", addr)
	return &ModuleInstance{
		Addr:   addr,
		Module: module,
		Call:   call,

		Variables:       map[addrs.InputVariable]*VariableInstance{},
		Resources:       map[addrs.Resource]ResourceInstances{},
		Calls:           map[addrs.ModuleCall]*ModuleCallInstances{},
		OutputInstances: map[addrs.OutputValue]*OutputInstance{},
	}
}
