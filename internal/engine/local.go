package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type LocalPlan struct {
	ValuePromise
}

func NewLocalValidate(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) ValuePromise {
	value := NewLocal(ctx, addr, config, scope, walkValidate)
	return value
}

func NewLocalPlan(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) LocalPlan {
	return LocalPlan{NewLocal(ctx, addr, config, scope, walkPlan)}
}

func NewLocalApply(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) (*Promise[cty.Value], Apply, tfdiags.Diagnostics) {
	return NewLocal(ctx, addr, config, scope, walkApply), nil, nil
}

func NewLocal(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope, op WalkOperation) *Promise[cty.Value] {
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

	return local
}
