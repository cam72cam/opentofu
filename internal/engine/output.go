package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewOutput(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, priorChanges *plans.Changes, priorState *states.State, scope *Scope, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {
	type Output struct {
		state  *states.OutputValue
		change *plans.OutputChangeSrc
	}

	output := NewPromise[Output](addr, func(self promise) (Output, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		// Make sure previous change is recorded
		if change := priorChanges.OutputValue(addr); change != nil {
			evalCtx.Changes().AppendOutputChange(change)
		}
		if state := priorState.OutputValue(addr); state != nil {
			evalCtx.State().SetOutputValue(addr, state.Value, state.Sensitive, state.Deprecated)
		}

		// TODO NodeDestroyableOutput
		node := &tofu.NodeApplyableOutput{
			Addr:   addr,
			Config: config,
			Change: priorChanges.OutputValue(addr),
			//TODO RefreshOnly:  o.RefreshOnly,
			DestroyApply: op == walkDestroy || op == walkPlanDestroy,
			Planning:     true, // Always true in the rest of the code base
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(op))

		return Output{
			evalCtx.State().OutputValue(node.Addr),
			evalCtx.Changes().GetOutputChange(addr),
		}, diags
	})

	// TODO better promise refinement
	outputValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		out, diags := output.Value(self)
		if out.state != nil {
			return out.state.Value, diags
		}
		return cty.NilVal, diags
	})

	action := func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
		out, diags := output.Value(nil)
		if out.state != nil {
			state.SetOutputValue(addr, out.state.Value, out.state.Sensitive, out.state.Deprecated)
		}
		if out.change != nil {
			change.AppendOutputChange(out.change)
		}
		return diags
	}

	return outputValue, action, nil
}
