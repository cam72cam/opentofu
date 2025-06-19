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

type PlanOutput struct {
	PrevRun *states.State
	Refresh *states.State
	State   *states.State
	Changes *plans.Changes
}

func WalkPlan(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, workspace string, state *states.State, inputs tofu.InputValues) (PlanOutput, tfdiags.Diagnostics) {
	if state == nil {
		state = states.NewState()
	}

	out := PlanOutput{
		PrevRun: state.DeepCopy(),
		Refresh: state.DeepCopy(),
		State:   state.DeepCopy(),
		Changes: plans.NewChanges(),
	}

	scope := NewRootScope(walkPlan, plugins, hooks, workspace, out.PrevRun.SyncWrapper(), out.Refresh.SyncWrapper(), out.State.SyncWrapper(), out.Changes.SyncWrapper(), config)

	root := NewModule(ctx, addrs.RootModuleInstance, config, NewRootVariableInputs(inputs), scope)

	p := NewConcurrencyPool(10)
	root.Collect(p)
	edges, diags := p.Wait()
	//spew.Dump(edges)
	fmt.Printf("Detected %v edges", len(edges))
	return out, diags
}
