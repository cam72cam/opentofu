package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/dag"
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
	fmt.Printf("Detected %v edges\n", len(edges))

	out.Checks = scope.Checks

	// See context_plan.go
	// The refreshed state may have data resource objects which were deferred
	// to apply and cannot be serialized.
	out.Refresh.SyncWrapper().RemovePlannedResourceInstanceObjects()

	// Post-process plan to spread create_before_destroy
	cbds := map[string]PoolEntry{}
	for _, mod := range out.Refresh.Modules {
		for _, res := range mod.Resources {
			for key, inst := range res.Instances {
				addr := res.Addr.Instance(key)
				if inst.Current.CreateBeforeDestroy {
					fmt.Printf("CBD: %s\n", addr.String())
					//TODO this is copy paste for now
					cbds[addr.String()] = nil
				}
			}
		}
	}

	var g dag.AcyclicGraph
	for _, e := range edges {
		g.Add(e.Requester)
		g.Add(e.Requestee)
		g.Connect(dag.BasicEdge(e.Requester, e.Requestee))

		_, ok := cbds[e.Requester.Ident().Addr().String()]
		if ok {
			cbds[e.Requester.Ident().Addr().String()] = e.Requester
		}
	}
	//println(g.StringWithNodeTypes())
	for _, cbd := range cbds {
		prop, _ := g.Ancestors(cbd)
		for _, p := range prop {
			id := p.(PoolEntry).Ident()
			if strings.HasSuffix(id.String(), "(instance)") {
				if addr, ok := id.Addr().(addrs.AbsResourceInstance); ok {
					println("Prop -> " + id.String())

					out.Refresh.ResourceInstance(addr).Current.CreateBeforeDestroy = true
					change := out.Changes.ResourceInstance(addr)
					if change.ChangeSrc.Action == plans.DeleteThenCreate {
						change.ChangeSrc.Action = plans.CreateThenDelete
					}
				}
			}
		}
	}

	return out, diags
}
