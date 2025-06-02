package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/states"
)

type Resource struct{}

type ResourceInstances map[addrs.InstanceKey]*ResourceInstance

type ResourceInstance struct {
	Addr           addrs.AbsResourceInstance
	Resource       *Resource
	RepetitionData instances.RepetitionData
	PreviousState  *states.ResourceInstance
}
