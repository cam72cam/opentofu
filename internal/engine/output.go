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

func NewOutputValidate(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, scope *Scope) (*Promise[cty.Value], Validate, tfdiags.Diagnostics) {
	value := NewPromise[cty.Value](addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
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
	return value, ValidatePromise(value), nil
}

func NewOutputPlan(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, priorState *states.State, scope *Scope) (*Promise[cty.Value], Plan, tfdiags.Diagnostics) {
	type Output struct {
		state   *states.OutputValue
		refresh *states.OutputValue
		change  *plans.OutputChangeSrc
	}
	output := NewPromise[Output](addr, func(self promise) (Output, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		if state := priorState.OutputValue(addr); state != nil {
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

		return Output{
			state:   evalCtx.State().OutputValue(node.Addr),
			refresh: evalCtx.RefreshState().OutputValue(node.Addr),
			change:  evalCtx.Changes().GetOutputChange(addr),
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

	plan := func(data PlanData) tfdiags.Diagnostics {
		out, diags := output.Value(nil)
		if out.state != nil {
			data.State.SetOutputValue(addr, out.state.Value, out.state.Sensitive, out.state.Deprecated)
		}
		if out.refresh != nil {
			data.Refresh.SetOutputValue(addr, out.refresh.Value, out.refresh.Sensitive, out.refresh.Deprecated)
		}
		if out.change != nil {
			data.Changes.AppendOutputChange(out.change)
		}
		return diags
	}

	return outputValue, plan, nil
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
