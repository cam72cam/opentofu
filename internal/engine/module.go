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

type ModuleCall struct {
	Addr   addrs.Module
	Config *configs.ModuleCall
	Parent *Module

	Module *Module

	Status     Status
	Dependents []struct{}

	InstancesByPath ModuleCallInstancesByPath
}

type Module struct {
	Addr   addrs.Module
	Call   *ModuleCall
	Config *configs.Config

	Variables map[string]*Variable
	Calls     map[string]*ModuleCall
	Resources map[string]*Resource
	Outputs   map[string]*Output
}

type ModuleCallInstancesByPath map[string]ModuleCallInstances

// TODO struct
type ModuleCallInstances map[addrs.InstanceKey]*ModuleCallInstance

type ModuleCallInstance struct {
	ModuleInstance *ModuleInstance
	RepetitionData instances.RepetitionData
}

type ModuleInstance struct {
	Addr   addrs.ModuleInstance
	Module *Module

	Variables       map[string]*VariableInstance
	Resources       map[string]ResourceInstances
	OutputInstances map[string]*OutputInstance
}

func NewModuleCall(addr addrs.Module, call *configs.ModuleCall, parent *Module, config *configs.Config) *ModuleCall {
	fmt.Printf("Creating module call %s\n", addr)
	mc := &ModuleCall{
		Addr:            addr,
		Config:          call,
		Parent:          parent,
		InstancesByPath: make(ModuleCallInstancesByPath),
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

		Variables: map[string]*Variable{},
		Calls:     map[string]*ModuleCall{},
		Resources: map[string]*Resource{},
		Outputs:   map[string]*Output{},
	}

	if config == nil {
		return m
	}

	for name, call := range config.Module.ModuleCalls {
		m.Calls[name] = NewModuleCall(addr.Child(name), call, m, config.Children[name])
	}

	return m
}

func NewModuleInstance(addr addrs.ModuleInstance, module *Module) *ModuleInstance {
	fmt.Printf("Creating module instance %s\n", addr)
	return &ModuleInstance{
		Addr:   addr,
		Module: module,

		Variables:       map[string]*VariableInstance{},
		Resources:       map[string]ResourceInstances{},
		OutputInstances: map[string]*OutputInstance{},
	}
}
