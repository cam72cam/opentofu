package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/instances"
)

type Status int

const (
	StatusUnknown   = 0
	StatusPending   = 1
	StatusAvailable = 2
)

type Root struct {
	Module *Module
}

type ModuleCalls struct {
	Caller    *Module
	Config    *configs.ModuleCall
	Instances map[addrs.InstanceKey]*Module
}

type Module struct {
	Addr           addrs.ModuleInstance
	Call           *ModuleCalls
	RepetitionData *instances.RepetitionData
	Config         *configs.Module

	Variables   map[addrs.InputVariable]*Variable
	Locals      map[addrs.LocalValue]*Local
	ModuleCalls map[addrs.ModuleCall]*ModuleCalls
	Resources   map[addrs.Resource]*Resources
	Outputs     map[addrs.OutputValue]*Output
}

func NewModuleCalls(caller *Module, config *configs.ModuleCall) *ModuleCalls {
	return &ModuleCalls{
		Caller:    caller,
		Config:    config,
		Instances: map[addrs.InstanceKey]*Module{},
	}
}

func NewModule(addr addrs.ModuleInstance, call *ModuleCalls) *Module {
	fmt.Printf("Creating module instance %s\n", addr)
	return &Module{
		Addr: addr,
		Call: call,

		Variables:   map[addrs.InputVariable]*Variable{},
		Locals:      map[addrs.LocalValue]*Local{},
		Resources:   map[addrs.Resource]*Resources{},
		ModuleCalls: map[addrs.ModuleCall]*ModuleCalls{},
		Outputs:     map[addrs.OutputValue]*Output{},
	}
}
