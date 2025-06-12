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
	scope := NewRootScope(walkPlan, plugins, hooks)

	if state == nil {
		state = states.NewState()
	}

	root := NewModulePlan(ctx, addrs.RootModuleInstance, config, inputs, state, scope)

	out := PlanOutput{
		PrevRun: states.NewState(),
		Refresh: states.NewState(),
		State:   states.NewState(),
		Changes: plans.NewChanges(),
	}

	data, diags := root.PlanData()
	for _, m := range data.PrevRun {
		out.PrevRun.Modules[m.Addr.String()] = m
	}
	for _, m := range data.Refresh {
		out.Refresh.Modules[m.Addr.String()] = m
	}
	for _, m := range data.State {
		out.State.Modules[m.Addr.String()] = m
	}

	out.Changes.Resources = data.Resources
	out.Changes.Outputs = data.Outputs

	return out, diags
}
