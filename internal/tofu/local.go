package tofu

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	
	"github.com/zclconf/go-cty/cty"
)

func NewLocal(ctx context.Context, addr addrs.AbsLocalValue, config *configs.Local, scope *Scope) ValuePromise {
	return NewPromise(Ident{base: addr}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		node := &NodeLocal{
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
