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
	return NewPromise(addr, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		node := &tofu.NodeLocal{
			Addr:   addr,
			Config: config,
		}

		evalCtx, diags := scope.LegacyExecute(ctx, self, node)
		if diags.HasErrors() {
			return cty.NilVal, diags
		}
		return evalCtx.State().LocalValue(addr), diags
	})
}
