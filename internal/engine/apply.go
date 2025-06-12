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

type ApplyData struct {
	State *states.SyncState
}

type Apply func(ApplyData) tfdiags.Diagnostics

type Applys []Apply

func (s Applys) Collect(data ApplyData) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	for _, p := range s {
		diags = diags.Append(p(data))
	}
	return diags
}

func WalkApply(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, changes *plans.Changes, state *states.State, inputs VariableInputs) (*states.State, tfdiags.Diagnostics) {
	scope := NewRootScope(walkApply, plugins, hooks)

	if state == nil {
		state = states.NewState()
	}

	_, apply, diags := NewModuleApply(ctx, addrs.RootModuleInstance, config, inputs, changes, state, scope)

	newState := state.DeepCopy()
	data := ApplyData{
		State: newState.SyncWrapper(),
	}
	diags = diags.Append(apply(data))

	return newState, diags
}
