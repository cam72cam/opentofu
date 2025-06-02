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
			instance, ok = call.Instances.Get(traverseAddr, step.InstanceKey)
			if !ok {
				instance = &ModuleCallInstance{
					ModuleInstance: NewModuleInstance(traverseAddr, call.Module),
				}
				call.Instances.Set(traverseAddr, step.InstanceKey, instance)
			}

			traverse = call
			traverseAddr = append(traverseAddr, step)
		}

		for _, stateResource := range module.Resources {
			ident := stateResource.Addr.Resource.String()
			resources, ok := instance.ModuleInstance.Resources[ident]
			if !ok {
				resources = NewResourceInstances(nil)
				instance.ModuleInstance.Resources[ident] = resources
			}

			for resKey, stateInstance := range stateResource.Instances {
				resInst, ok := resources.Instances[resKey]
				if !ok {
					resInst = &ResourceInstance{}
					resources.Instances[resKey] = resInst
				}
				resInst.PreviousState = stateInstance
			}

		}
	}
}
