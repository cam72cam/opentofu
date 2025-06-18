package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/plugins"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
)

func WalkApply(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, workspace string, changes *plans.Changes, state *states.State, inputs VariableInputs) (*states.State, tfdiags.Diagnostics) {
	if state == nil {
		state = states.NewState()
	}

	scope := NewRootScope(walkApply, plugins, hooks, workspace, state.DeepCopy().SyncWrapper(), state.DeepCopy().SyncWrapper(), state.SyncWrapper(), changes.SyncWrapper())
	root := NewModule(ctx, addrs.RootModuleInstance, config, inputs, scope)

	p := NewConcurrencyPool(10)
	root.Collect(p)
	return state, p.Wait()
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
