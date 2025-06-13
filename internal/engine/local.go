package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewLocal(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) ValuePromise {
	return NewPromise(addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		node := &tofu.NodeLocal{
			Addr:   addr,
			Config: config,
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(scope.op))
		return evalCtx.State().LocalValue(addr), diags
	})
}
