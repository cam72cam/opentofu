package engine

// walkOperation is an enum which tells the walkContext what to do.
type WalkOperation byte

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
