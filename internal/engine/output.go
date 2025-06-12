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

type OutputPlanData struct {
	prevRun *states.OutputValue
	refresh *states.OutputValue
	state   *states.OutputValue
	change  *plans.OutputChangeSrc
}
type OutputPlan struct {
	promise *Promise[OutputPlanData]
}

func (o OutputPlan) Value(caller promise) (cty.Value, tfdiags.Diagnostics) {
	data, diags := o.promise.Value(caller)
	if data.state != nil {
		return data.state.Value, diags
	}
	return cty.NilVal, diags
}

func NewOutputValidate(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, scope *Scope) *Promise[cty.Value] {
	return NewPromise[cty.Value](addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)
		node := &tofu.NodeApplyableOutput{
			Addr:         addr,
			Config:       config,
			DestroyApply: false,
			Planning:     true, // Always true in the rest of the code base
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkValidate))

		var result cty.Value
		if out := evalCtx.State().OutputValue(addr); out != nil {
			result = out.Value
		}

		return result, diags
	})
}

func NewOutputPlan(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, state *states.OutputValue, scope *Scope) OutputPlan {
	return OutputPlan{
		NewPromise[OutputPlanData](addr, func(self promise) (OutputPlanData, tfdiags.Diagnostics) {
			evalCtx := scope.EvalContext(self)

			if state != nil {
				evalCtx.RefreshState().SetOutputValue(addr, state.Value, state.Sensitive, state.Deprecated)
				evalCtx.State().SetOutputValue(addr, state.Value, state.Sensitive, state.Deprecated)
			}

			// TODO NodeDestroyableOutput
			node := &tofu.NodeApplyableOutput{
				Addr:   addr,
				Config: config,
				//TODO RefreshOnly:  o.RefreshOnly,
				DestroyApply: false, // TODO op == walkDestroy || op == walkPlanDestroy,
				Planning:     true,  // Always true in the rest of the code base
			}
			diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkPlan))

			return OutputPlanData{
				prevRun: state,
				refresh: evalCtx.RefreshState().OutputValue(node.Addr),
				state:   evalCtx.State().OutputValue(node.Addr),
				change:  evalCtx.Changes().GetOutputChange(addr),
			}, diags
		}),
	}
}

func NewOutputApply(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, priorChanges *plans.Changes, priorState *states.State, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {
	output := NewPromise[*states.OutputValue](addr, func(self promise) (*states.OutputValue, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

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
			DestroyApply: false, // TODO op == walkDestroy || op == walkApplyDestroy,
			Planning:     true,  // Always true in the rest of the code base
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkApply))

		return evalCtx.State().OutputValue(node.Addr), diags
	})

	// TODO better promise refinement
	outputValue := NewPromise[cty.Value](&addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		out, diags := output.Value(self)
		if out != nil {
			return out.Value, diags
		}
		return cty.NilVal, diags
	})

	apply := func(data ApplyData) tfdiags.Diagnostics {
		out, diags := output.Value(nil)
		if out != nil {
			data.State.SetOutputValue(addr, out.Value, out.Sensitive, out.Deprecated)
		}
		return diags
	}

	return outputValue, apply, nil
}
