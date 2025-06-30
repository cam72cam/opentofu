package engine

import (
	"context"
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
)

type PlanOutput struct {
	PrevRun *states.State
	Refresh *states.State
	State   *states.State
	Changes *plans.Changes

	Checks *checks.State
}

func WalkPlan(ctx context.Context, config *configs.Config, tofuCtx *tofu.Context, state *states.State, inputs tofu.InputValues) (PlanOutput, tfdiags.Diagnostics) {
	if state == nil {
		state = states.NewState()
	}

	out := PlanOutput{
		PrevRun: state.DeepCopy(),
		Refresh: state.DeepCopy(),
		State:   state.DeepCopy(),
		Changes: plans.NewChanges(),
	}

	scope := NewRootScope(walkPlan, tofuCtx, out.PrevRun, out.Refresh, out.State, out.Changes, config)

	root := NewModule(ctx, addrs.RootModuleInstance, config, NewRootVariableInputs(inputs), scope)

	p := NewManager(tofuCtx.Semaphore())
	root.Collect(p)
	edges, diags := p.Wait()
	//spew.Dump(edges)
	fmt.Printf("Detected %v edges", len(edges))

	out.Checks = scope.Checks

	// See context_plan.go
	// The refreshed state may have data resource objects which were deferred
	// to apply and cannot be serialized.
	out.Refresh.SyncWrapper().RemovePlannedResourceInstanceObjects()

	return out, diags
}
