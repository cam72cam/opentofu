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

type PlanOutput struct {
	PrevRun *states.State
	Refresh *states.State
	State   *states.State
	Changes *plans.Changes
}

func WalkPlan(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, state *states.State, inputs VariableInputs) (PlanOutput, tfdiags.Diagnostics) {
	if state == nil {
		state = states.NewState()
	}

	out := PlanOutput{
		PrevRun: state.DeepCopy(),
		Refresh: state.DeepCopy(),
		State:   state.DeepCopy(),
		Changes: plans.NewChanges(),
	}

	scope := NewRootScope(walkPlan, plugins, hooks, out.PrevRun.SyncWrapper(), out.Refresh.SyncWrapper(), out.State.SyncWrapper(), out.Changes.SyncWrapper())

	root := NewModule(ctx, addrs.RootModuleInstance, config, inputs, scope)

	p := NewConcurrencyPool(10)
	root.Collect(p)
	return out, p.Wait()
}
