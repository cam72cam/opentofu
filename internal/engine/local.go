package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewLocalValidate(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) (*Promise[cty.Value], Validate, tfdiags.Diagnostics) {
	value, diags := NewLocal(ctx, addr, config, scope, walkValidate)
	return value, ValidatePromise(value), diags
}

func NewLocalPlan(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) (*Promise[cty.Value], Plan, tfdiags.Diagnostics) {
	value, diags := NewLocal(ctx, addr, config, scope, walkPlan)
	return value, nil, diags
}

func NewLocalApply(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {
	value, diags := NewLocal(ctx, addr, config, scope, walkApply)
	return value, nil, diags
}

func NewLocal(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope, op WalkOperation) (*Promise[cty.Value], tfdiags.Diagnostics) {
	local := NewPromise[cty.Value](addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		node := &tofu.NodeLocal{
			Addr:   addr,
			Config: config,
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(op))
		return evalCtx.State().LocalValue(addr), diags
	})

	/*
		action := func(_ *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
			if op == walkEval {
				val, diags := local.Value(nil)
				state.SetLocalValue(addr, val)
				return diags
			}
			return nil
		}*/

	return local, nil
}
