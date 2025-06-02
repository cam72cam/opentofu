package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/states"
)

type Resource struct{}

type ResourceInstances struct {
	Resource  *Resource
	Instances map[addrs.InstanceKey]*ResourceInstance
	// TODO for_each
}

func NewResourceInstances(Resource *Resource) *ResourceInstances {
	return &ResourceInstances{
		Instances: map[addrs.InstanceKey]*ResourceInstance{},
	}
}

type ResourceInstance struct {
	Resource      *ResourceInstances
	PreviousState *states.ResourceInstance
}
