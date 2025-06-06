package engine

import "github.com/opentofu/opentofu/internal/tofu"

// walkOperation is an enum which tells the walkContext what to do.
type WalkOperation tofu.WalkOperation

const (
	walkInvalid WalkOperation = iota
	walkApply
	walkPlan
	walkPlanDestroy
	walkValidate
	walkDestroy
	walkImport
	walkEval // used just to prepare EvalContext for expression evaluation, with no other actions
)

/* TODO
func NewEvalContext(mod *Module, source any) tofu.EvalContext {
	// Use mock for now helper struct later
	return &tofu.MockEvalContext{
		PathPath:          mod.Addr,
		StateState:        states.NewState().SyncWrapper(),
		RefreshStateState: states.NewState().SyncWrapper(),
		ChecksState:       checks.NewState(nil),
		EvaluateExprResultFunc: func(
			expr hcl.Expression,
			wantType cty.Type,
			self addrs.Referenceable,
		) (cty.Value, tfdiags.Diagnostics) {
			refs, diags := lang.ReferencesInExpr(addrs.ParseRef, expr)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			scope, diags := mod.ScopeForReferences(refs, []any{source})
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			value, valueDiags := scope.EvalExpr(expr, wantType)
			return value, diags.Append(valueDiags)
		},
	}
}*/
