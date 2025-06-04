package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/states"
)

type Resources struct {
	Module *Module
	Config *configs.Resource

	Instances map[addrs.InstanceKey]*Resource
}

func NewResources(m *Module, config *configs.Resource) *Resources {
	return &Resources{
		Module:    m,
		Config:    config,
		Instances: map[addrs.InstanceKey]*Resource{},
	}
}

type Resource struct {
	Addr   addrs.AbsResourceInstance
	Config *configs.Resource
	Module *Module

	RepetitionData *instances.RepetitionData
	PreviousState  *states.ResourceInstance
}
