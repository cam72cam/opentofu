package tofu

import (
	"context"
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

type PlanOutput struct {
	PrevRun *states.State
	Refresh *states.State
	State   *states.State
	Changes *plans.Changes

	Checks *checks.State
}

func WalkPlan(ctx context.Context, config *configs.Config, tofuCtx *Context, state *states.State, inputs InputValues) (PlanOutput, tfdiags.Diagnostics) {
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

	p := NewManager(tofuCtx.parallelSem)
	root.Collect(p)
	edges, diags := p.Wait()
	//spew.Dump(edges)
	fmt.Printf("Detected %v edges\n", len(edges))

	out.Checks = scope.Checks

	// Post-process plan to spread create_before_destroy

	// Figure out which resources are *actively* CreateThenDelete
	propagateTo := addrs.Set[addrs.ConfigResource]{}
	for _, change := range out.Changes.Resources {
		if change.ChangeSrc.Action == plans.CreateThenDelete {
			println("CTD: " + change.Addr.String())

			changeState := out.Refresh.ResourceInstance(change.Addr)

			if changeState != nil {
				for _, dep := range changeState.Current.Dependencies {
					propagateTo.Add(dep)
				}
			}
		}
	}

	for _, target := range propagateTo {
		for _, res := range state.Resources(target) {
			for key, inst := range res.Instances {
				// Legacy marker, not really used anymore
				inst.Current.CreateBeforeDestroy = true

				depChange := out.Changes.ResourceInstance(res.Addr.Instance(key))
				if depChange != nil && depChange.ChangeSrc.Action == plans.DeleteThenCreate {
					depChange.ChangeSrc.Action = plans.CreateThenDelete
				}
			}
		}
	}

	return out, diags
}
