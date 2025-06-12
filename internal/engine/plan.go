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

type PlanData struct {
	// Copy of the previous state, with provider resource upgrades applied
	// Used for UIOutput / diffing
	PrevRun *states.SyncState
	// Copy of the previous state, with provider resource upgrades and resource refreshes applied
	// Used as the Apply input state (Apply does not refresh)
	Refresh *states.SyncState
	// Copy of the previous state, with all expected changes applied
	// Used for UIOutput / diffing
	State   *states.SyncState
	Changes *plans.ChangesSync
}

type Plan func(PlanData) tfdiags.Diagnostics

type Plans []Plan

func (s Plans) Collect(data PlanData) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	for _, p := range s {
		diags = diags.Append(p(data))
	}
	return diags
}

type PlanOutput struct {
	PrevRun *states.State
	Refresh *states.State
	State   *states.State
	Changes *plans.Changes
}

func WalkPlan(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, state *states.State, inputs VariableInputs) (PlanOutput, tfdiags.Diagnostics) {
	scope := NewRootScope(walkPlan, plugins, hooks)

	if state == nil {
		state = states.NewState()
	}

	_, plan, diags := NewModulePlan(ctx, addrs.RootModuleInstance, config, inputs, state, scope)

	out := PlanOutput{
		PrevRun: state.DeepCopy(),
		Refresh: state.DeepCopy(),
		State:   state.DeepCopy(),
		Changes: plans.NewChanges(),
	}

	data := PlanData{
		// See tofu/context_walk.go
		PrevRun: out.PrevRun.SyncWrapper(),
		Refresh: out.Refresh.SyncWrapper(),
		State:   out.State.SyncWrapper(),
		Changes: out.Changes.SyncWrapper(),
	}
	diags = diags.Append(plan(data))

	return out, diags
}
