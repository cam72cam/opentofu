package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states"
)

func NewRoot(config *configs.Config) *Root {
	unexpanded := NewModuleCall(addrs.RootModule, nil, config)
	expanded := NewModuleCallInstance(NewModuleInstance(addrs.RootModuleInstance, unexpanded.Module))
	// TODO var inputs

	return &Root{
		ModuleCall:         unexpanded,
		ModuleCallInstance: expanded,
	}
}

func (root *Root) AttachState(state *states.State) {
	println("Attaching state")
	for _, module := range state.Modules {
		var traverseAddr addrs.ModuleInstance
		var instance *ModuleCallInstance

		unexpanded := root.ModuleCall
		expanded := root.ModuleCallInstance
		for _, step := range module.Addr {
			// Ensure unexpanded tree has the required module
			call, ok := unexpanded.Module.Calls[step.Name]
			if !ok {
				call = NewModuleCall(unexpanded.Addr.Child(step.Name), nil, nil)
				unexpanded.Module.Calls[step.Name] = call
			}

			// Ensure expanded tree has the required module
			traverseAddr = append(traverseAddr, step)

			calls, ok := expanded.ModuleInstance.Calls[step.Name]
			if !ok {
				calls = NewModuleCallInstances(expanded.ModuleInstance.Addr, call)
				expanded.ModuleInstance.Calls[step.Name] = calls
			}

			instance, ok = calls.Instances[step.InstanceKey]
			if !ok {
				instance = NewModuleCallInstance(NewModuleInstance(expanded.ModuleInstance.Addr.Child(step.Name, step.InstanceKey), call.Module))
				calls.Instances[step.InstanceKey] = instance
			}

			unexpanded = call
			expanded = instance
		}

		for path, stateResource := range module.Resources {
			fmt.Printf("Attaching resource: %s\n", path)
			ident := stateResource.Addr.Resource.String()
			resources, ok := instance.ModuleInstance.Resources[ident]
			if !ok {
				resources = ResourceInstances{}
				instance.ModuleInstance.Resources[ident] = resources
			}

			for resKey, stateInstance := range stateResource.Instances {
				fmt.Printf("Attaching Resource Instance: %s\n", stateResource.Addr)
				resInst, ok := resources[resKey]
				if !ok {
					resInst = &ResourceInstance{
						Addr: stateResource.Addr.Instance(resKey),
					}
					resources[resKey] = resInst
				}
				resInst.PreviousState = stateInstance
			}
		}
	}
}
