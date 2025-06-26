package engine

import (
	"context"
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/plugins"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
)

func WalkApply(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, workspace string, changes *plans.Changes, state *states.State, checks *states.CheckResults, inputs tofu.InputValues) (*states.State, tfdiags.Diagnostics) {
	if state == nil {
		state = states.NewState()
	}

	scope := NewRootScope(walkApply, plugins, hooks, workspace, state.DeepCopy().SyncWrapper(), state.DeepCopy().SyncWrapper(), state.SyncWrapper(), changes.SyncWrapper(), config)

	for _, configElem := range checks.ConfigResults.Elems {
		if configElem.Value.ObjectAddrsKnown() {
			configAddr := configElem.Key
			scope.Checks.ReportCheckableObjects(configAddr, configElem.Value.ObjectResults.Keys())
		}
	}

	root := NewModule(ctx, addrs.RootModuleInstance, config, NewRootVariableInputs(inputs), scope)

	p := NewManager()
	root.Collect(p)
	edges, diags := p.Wait()
	fmt.Printf("Detected %v edges", len(edges))
	//spew.Dump(edges)
	return state, diags
}

/*

type ApplyData struct {
	changes *plans.Changes
	state   *states.SyncState
	sync    func(state *states.State)
}

type ModuleApplyData struct {
	Addr addrs.ModuleInstance
	data ApplyData
}

func (m *ModuleApplyData) Child(call addrs.ModuleCallInstance) *ModuleApplyData {
	return &ModuleApplyData{
		Addr: call.Absolute(m.Addr),
		data: m.data,
	}
}

func (m *ModuleApplyData) Output(addr addrs.OutputValue) *OutputApplyData {
	return &OutputApplyData{
		Addr: addr.Absolute(m.Addr),
		data: m.data,
	}
}

type OutputApplyData struct {
	Addr addrs.AbsOutputValue
	data ApplyData
}

func (o *OutputApplyData) GetChange() {
	return
}
func (o *OutputApplyData) GetChangeAndExpected() (*states.OutputValue, *plans.OutputChangeSrc)  {
	o.changes.OutputValue(o.Addr)
	return o.state.OutputValue(o.Addr)
}
func (o *OutputApplyData) SetValue(value *states.OutputValue) {
	if o.GetValue() != value {

	}
}*/
