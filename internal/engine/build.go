package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states"
)

func Build(config *configs.Config) *ModuleCall {
	return NewModuleCall(addrs.RootModule, nil, nil, config)
}

func AttachState(root *ModuleCall, state *states.State) {
	for _, module := range state.Modules {
		traverse := root
		var traverseAddr addrs.ModuleInstance
		var instance *ModuleCallInstance
		for _, step := range module.Addr {
			call, ok := traverse.Module.Calls[step.Name]
			if !ok {
				call = NewModuleCall(traverse.Addr.Child(step.Name), nil, nil, nil)
				traverse.Module.Calls[step.Name] = call
			}
			instances, ok := call.InstancesByPath[traverseAddr.String()]
			if !ok {
				instances = ModuleCallInstances{}
			}
			instance, ok = instances[step.InstanceKey]
			if !ok {
				instance = &ModuleCallInstance{
					ModuleInstance: NewModuleInstance(traverseAddr, call.Module),
				}
				instances[step.InstanceKey] = instance
			}

			traverse = call
			traverseAddr = append(traverseAddr, step)
		}

		for _, stateResource := range module.Resources {
			ident := stateResource.Addr.Resource.String()
			resources, ok := instance.ModuleInstance.Resources[ident]
			if !ok {
				resources = ResourceInstances{}
				instance.ModuleInstance.Resources[ident] = resources
			}

			for resKey, stateInstance := range stateResource.Instances {
				resInst, ok := resources[resKey]
				if !ok {
					resInst = &ResourceInstance{}
					resources[resKey] = resInst
				}
				resInst.PreviousState = stateInstance
			}

		}
	}
}
