package engine

/*
type Status int

const (
	StatusUnknown   = 0
	StatusPending   = 1
	StatusAvailable = 2
)

type ModuleCalls struct {
	Caller    *Module
	Config    *configs.ModuleCall
	Instances map[addrs.InstanceKey]*Module

	VariableValues map[addrs.InputVariable]*Promise[cty.Value]
}

func NewModuleCalls(caller *Module, config *configs.ModuleCall, vars map[addrs.InputVariable]*Promise[cty.Value]) *ModuleCalls {
	return &ModuleCalls{
		Caller:         caller,
		Config:         config,
		Instances:      map[addrs.InstanceKey]*Module{},
		VariableValues: vars,
	}
}

func (m *ModuleCalls) Value() (cty.Value, tfdiags.Diagnostics) {
	return m.value()
}
func (m *ModuleCalls) value() (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	if m.Config == nil {
		panic("asked for the value of a unconfigured module call!")
	}

	// TODO parallel execution of fetching module info
	moduleInstances := make(map[addrs.InstanceKey]cty.Value)
	for key, mod := range m.Instances {
		var modDiags tfdiags.Diagnostics
		moduleInstances[key], modDiags = mod.Value()
		diags = diags.Append(modDiags)
	}
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	// Lifted from tofu/evaluate.go

	switch {
	case m.Config.Count != nil:
		length := -1
		for key := range moduleInstances {
			intKey, ok := key.(addrs.IntKey)
			if !ok {
				// old key from state which is being dropped
				continue
			}
			if int(intKey) >= length {
				length = int(intKey) + 1
			}
		}

		if length <= 0 {
			return cty.EmptyTupleVal, diags
		}
		vals := make([]cty.Value, length)
		for key, instance := range moduleInstances {
			intKey, ok := key.(addrs.IntKey)
			if !ok {
				// old key from state which is being dropped
				continue
			}

			vals[int(intKey)] = instance
		}

		// Insert unknown values where there are any missing instances
		for i, v := range vals {
			if v.IsNull() {
				vals[i] = cty.DynamicVal
				continue
			}
		}
		return cty.TupleVal(vals), diags

	case m.Config.ForEach != nil:
		instanceMap := make(map[string]cty.Value)
		for key, mod := range moduleInstances {
			sk := key.(addrs.StringKey)
			instanceMap[string(sk)] = mod
		}
		return cty.ObjectVal(instanceMap), diags
	default:
		return moduleInstances[addrs.NoKey], diags
	}
}

type Module struct {
	Addr           addrs.ModuleInstance
	Call           *ModuleCalls
	RepetitionData *instances.RepetitionData
	Config         *configs.Config

	Variables   map[addrs.InputVariable]*Variable
	Locals      map[addrs.LocalValue]*Local
	ModuleCalls map[addrs.ModuleCall]*ModuleCalls
	Resources   map[addrs.Resource]*Resources
	Outputs     map[addrs.OutputValue]*Output
}

func NewModule(addr addrs.ModuleInstance, call *ModuleCalls, config *configs.Config) *Module {
	fmt.Printf("Creating module instance %s\n", addr)
	return &Module{
		Addr:   addr,
		Call:   call,
		Config: config,

		Variables:   map[addrs.InputVariable]*Variable{},
		Locals:      map[addrs.LocalValue]*Local{},
		Resources:   map[addrs.Resource]*Resources{},
		ModuleCalls: map[addrs.ModuleCall]*ModuleCalls{},
		Outputs:     map[addrs.OutputValue]*Output{},
	}
}

func (m *Module) Value() (cty.Value, tfdiags.Diagnostics) {
	return m.value()
}
func (m *Module) value() (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	if m.Config == nil {
		panic("asked for the value of a unconfigured module")
	}

	outputValues := make(map[string]cty.Value)

	// TODO parallel execution of fetching outputs
	for _, output := range m.Outputs {
		var outDiags tfdiags.Diagnostics
		outputValues[output.Config.Name], outDiags = output.Value()
		diags = diags.Append(outDiags)
	}
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	return cty.ObjectVal(outputValues), nil
}
*/
