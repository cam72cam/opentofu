package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states"
)

func NewRoot(config *configs.Config) *Root {
	unexpanded := NewModuleCall(addrs.RootModule, nil, config)
	expanded := NewModuleInstance(addrs.RootModuleInstance, unexpanded.Module, nil)
	// TODO var inputs

	return &Root{
		ModuleCall:     unexpanded,
		ModuleInstance: expanded,
	}
}

func (root *Root) AttachState(state *states.State) {
	println("Attaching state")
	for _, module := range state.Modules {
		var traverseAddr addrs.ModuleInstance
		var instance *ModuleInstance

		unexpanded := root.ModuleCall
		expanded := root.ModuleInstance
		for _, step := range module.Addr {
			// Ensure unexpanded tree has the required module
			call, ok := unexpanded.Module.Calls[addrs.ModuleCall{Name: step.Name}]
			if !ok {
				call = NewModuleCall(unexpanded.Addr.Child(step.Name), nil, nil)
				unexpanded.Module.Calls[addrs.ModuleCall{Name: step.Name}] = call
			}

			// Ensure expanded tree has the required module
			traverseAddr = append(traverseAddr, step)

			calls, ok := expanded.Calls[addrs.ModuleCall{Name: step.Name}]
			if !ok {
				calls = NewModuleCallInstances(expanded, call)
				expanded.Calls[addrs.ModuleCall{Name: step.Name}] = calls
			}

			instance, ok = calls.Instances[step.InstanceKey]
			if !ok {
				instance = NewModuleInstance(expanded.Addr.Child(step.Name, step.InstanceKey), call.Module, calls)
				calls.Instances[step.InstanceKey] = instance
			}

			unexpanded = call
			expanded = instance
		}

		for path, stateResource := range module.Resources {
			fmt.Printf("Attaching resource: %s\n", path)
			ident := stateResource.Addr.Resource
			resources, ok := instance.Resources[ident]
			if !ok {
				resources = ResourceInstances{}
				instance.Resources[ident] = resources
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
