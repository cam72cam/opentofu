package engine

import (
	"context"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

func NewOutput(ctx context.Context, addr addrs.AbsOutputValue, config *configs.Output, scope *Scope) ValuePromise {
	return NewPromise(addr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)

		planning := scope.op != walkApply //TODO plan graph only
		if planning {
			configAddr := addr.OutputValue.InModule(addr.Module.Module())
			if checkState := evalCtx.Checks(); checkState.ConfigHasChecks(configAddr) {
				checkState.ReportCheckableObject(configAddr, addr)
			}
		}

		// TODO NodeDestroyableOutput
		node := &tofu.NodeApplyableOutput{
			Addr:   addr,
			Config: config,
			//TODO RefreshOnly:  o.RefreshOnly,
			DestroyApply: false, // TODO op == walkDestroy || op == walkPlanDestroy,
			Planning:     planning,
		}
		diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(walkPlan))

		val := evalCtx.State().OutputValue(node.Addr)
		if val == nil {
			return cty.NilVal, diags
		}
		return val.Value, diags
	})
}
