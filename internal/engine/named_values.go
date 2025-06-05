package engine

/*
type Variable struct {
	Addr   addrs.AbsInputVariableInstance
	Module *Module
	Config *configs.Variable
}

func NewVariable(addr addrs.AbsInputVariableInstance, mod *Module, config *configs.Variable) *Variable {
	return &Variable{
		Addr:   addr,
		Module: mod,
		Config: config,
	}
}

func (v *Variable) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}

type Local struct {
	Addr   addrs.AbsLocalValue
	Module *Module
	Config *configs.Local
}

func NewLocal(addr addrs.AbsLocalValue, mod *Module, config *configs.Local) *Local {
	return &Local{
		Addr:   addr,
		Module: mod,
		Config: config,
	}
}

func (l *Local) Value() (cty.Value, tfdiags.Diagnostics) {
	return l.value()
}

func (l *Local) value() (cty.Value, tfdiags.Diagnostics) {
	evalCtx := NewEvalContext(l.Module, l)

	node := &tofu.NodeLocal{
		Addr:   l.Addr,
		Config: l.Config,
	}
	diags := node.Execute(context.TODO(), evalCtx, tofu.WalkOperation(walkPlan))
	return evalCtx.State().LocalValue(l.Addr), diags
}

type Output struct {
	Addr   addrs.AbsOutputValue
	Module *Module
	Config *configs.Output
}

func NewOutput(addr addrs.AbsOutputValue, mod *Module, config *configs.Output) *Output {
	return &Output{
		Addr:   addr,
		Module: mod,
		Config: config,
	}
}

func (o *Output) Value() (cty.Value, tfdiags.Diagnostics) {
	return o.value()
}
func (o *Output) value() (cty.Value, tfdiags.Diagnostics) {
	// TODO NodeDestroyableOutput
	node := &tofu.NodeApplyableOutput{
		Addr:   o.Addr,
		Config: o.Config,
		//Change:       change,
		//RefreshOnly:  o.RefreshOnly,
		//DestroyApply: o.Destroying,
		//Planning:     o.Planning,
	}

	evalCtx := NewEvalContext(o.Module, o)

	diags := node.Execute(context.TODO(), evalCtx, tofu.WalkOperation(walkPlan))
	return evalCtx.State().OutputValue(node.Addr).Value, diags
}
*/
