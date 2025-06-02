package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
)

/*

Tree of ModuleCall -> Module -*> ModuleCall -> Module etc...
With each ModuleCall serving as the expander tracker

Information Needed?
* AllKeys => AllInstances
* ActiveKeys => InstanceData


*/

type ModuleCall struct {
	Addr   addrs.Module
	Config *configs.ModuleCall
	Parent *Module

	Module *Module

	Instances InstanceMap[*ModuleCallInstance]
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

type ModuleInstance struct {
	Addr   addrs.ModuleInstance
	Module *Module

	Variables       map[string]*VariableInstance
	Calls           map[string]*ModuleCallInstance
	Resources       map[string]*ResourceInstances
	OutputInstances map[string]*OutputInstance
}
type ModuleCallInstance struct {
	ModuleInstance *ModuleInstance
	// TODO Known Keys
}

func NewModuleCall(addr addrs.Module, call *configs.ModuleCall, parent *Module, config *configs.Config) *ModuleCall {
	mc := &ModuleCall{
		Addr:      addr,
		Config:    call,
		Parent:    parent,
		Instances: NewInstanceMap[*ModuleCallInstance](),
	}
	mc.Module = NewModule(addr, mc, config)
	return mc
}

func NewModule(addr addrs.Module, call *ModuleCall, config *configs.Config) *Module {
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
	return &ModuleInstance{
		Addr:   addr,
		Module: module,

		Variables:       map[string]*VariableInstance{},
		Calls:           map[string]*ModuleCallInstance{},
		Resources:       map[string]*ResourceInstances{},
		OutputInstances: map[string]*OutputInstance{},
	}
}
