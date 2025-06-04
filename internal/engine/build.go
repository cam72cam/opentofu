package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states"
)

func NewRoot(config *configs.Config) *Root {
	expanded := NewModule(addrs.RootModuleInstance, nil)
	// TODO var inputs

	return &Root{
		Module: expanded,
	}
}

func (root *Root) AttachState(state *states.State) {
	println("Attaching state")
	for _, module := range state.Modules {
		var traverseAddr addrs.ModuleInstance
		var instance *Module

		target := root.Module
		for _, step := range module.Addr {
			// Ensure expanded tree has the required module
			traverseAddr = append(traverseAddr, step)

			calls, ok := target.ModuleCalls[addrs.ModuleCall{Name: step.Name}]
			if !ok {
				calls = NewModuleCalls(target, nil)
				target.ModuleCalls[addrs.ModuleCall{Name: step.Name}] = calls
			}

			instance, ok = calls.Instances[step.InstanceKey]
			if !ok {
				instance = NewModule(target.Addr.Child(step.Name, step.InstanceKey), calls)
				calls.Instances[step.InstanceKey] = instance
			}

			target = instance
		}

		for path, stateResource := range module.Resources {
			fmt.Printf("Attaching resource: %s\n", path)
			ident := stateResource.Addr.Resource
			resources, ok := instance.Resources[ident]
			if !ok {
				resources = NewResources(instance, nil)
				instance.Resources[ident] = resources
			}

			for resKey, state := range stateResource.Instances {
				fmt.Printf("Attaching Resource Instance: %s\n", stateResource.Addr)
				resInst, ok := resources.Instances[resKey]
				if !ok {
					resInst = &Resource{
						Addr: stateResource.Addr.Instance(resKey),
					}
					resources.Instances[resKey] = resInst
				}
				resInst.PreviousState = state
			}
		}
	}
}
