package engine

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type VariableInput struct {
	expr  hcl.Expression
	scope *Scope
}

type Variable struct {
	*Promise[cty.Value]
}

func NewVariable(ctx context.Context, addr addrs.AbsInputVariableInstance, config *configs.Variable, caller VariableInput, scope *Scope, op WalkOperation) (*Promise[cty.Value], Action, tfdiags.Diagnostics) {
	variable := NewPromise[cty.Value](addr, func(self promise) (cty.Value, tfdiags.Diagnostics) {
		evalCtx := scope.EvalContext(self)
		parentEvalCtx := caller.scope.EvalContext(self)

		// It is evaluated in the "parent" module
		input := &tofu.NodeModuleVariable{
			Addr:           addr,
			Config:         config,
			Expr:           caller.expr,
			ModuleInstance: addr.Module,
		}
		diags := input.Execute(ctx, parentEvalCtx, tofu.WalkOperation(op))
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		// HACK: Shift value from parent to child context (see mock hack)
		_, call := addr.Module.CallInstance()
		evalCtx.SetModuleCallArgument(
			call, addr.Variable,
			parentEvalCtx.GetVariableValue(addr),
		)

		// It is evaluated in the "child" module
		ref := &tofu.NodeVariableReferenceInstance{
			Addr:   addr,
			Config: config,
			Expr:   caller.expr,

			// TODO VariableFromRemoteModule: c.IsModuleCallFromRemoteModule(callConfig.Name),
		}
		execDiags := ref.Execute(ctx, evalCtx, tofu.WalkOperation(op))
		diags = diags.Append(execDiags)
		return parentEvalCtx.GetVariableValue(addr), diags
	})

	return variable, nil, nil

}
