package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/states"
)

type StateModule struct {
	Resources map[addrs.Resource]StateResource
	Calls     map[addrs.ModuleCall]StateCall
	Outputs   map[addrs.OutputValue]StateOutput
}

func NewStateModule(state states.State) *StateModule {
	root := &StateModule{
		Resources: map[addrs.Resource]StateResource{},
		Calls:     map[addrs.ModuleCall]StateCall{},
		Outputs:   map[addrs.OutputValue]StateOutput{},
	}

	//for _, mod := range state.Modules {

	/*m := &StateModule{
		Resources: map[addrs.Resource]StateResource{},
		Calls:     map[addrs.ModuleCall]StateCall{},
		Outputs:   map[addrs.Output]StateOutput{},
	}*/
	//}

	return root //mapping[addrs.RootModuleInstance.String()]
}

type StateCall map[addrs.InstanceKey]StateModule

type StateResource struct {
}

type StateOutput struct {
}
