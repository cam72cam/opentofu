package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type Scope struct {
	path      addrs.ModuleInstance
	op        WalkOperation
	expander  *instances.Expander
	variables map[addrs.InputVariable]*Promise[cty.Value]
	locals    map[addrs.LocalValue]*Promise[cty.Value]
	resources map[addrs.Resource]*Promise[cty.Value]
	calls     map[addrs.ModuleCall]*Promise[cty.Value]
	outputs   map[addrs.OutputValue]*Promise[cty.Value]
}

func NewScope(path addrs.ModuleInstance, op WalkOperation, parent *Scope) *Scope {
	var expander *instances.Expander
	if parent != nil {
		expander = parent.expander
	} else {
		expander = instances.NewExpander()
	}

	return &Scope{
		path:      path,
		op:        op,
		expander:  expander,
		variables: map[addrs.InputVariable]*Promise[cty.Value]{},
		locals:    map[addrs.LocalValue]*Promise[cty.Value]{},
		resources: map[addrs.Resource]*Promise[cty.Value]{},
		calls:     map[addrs.ModuleCall]*Promise[cty.Value]{},
		outputs:   map[addrs.OutputValue]*Promise[cty.Value]{},
	}
}

func (s *Scope) EvalContext(caller promise) tofu.EvalContext {
	// I think this can be stupid?
	// This is just a hack for the variable input passthrough from parent -> child in the variable nodes
	var varCache cty.Value

	evalCtx := &tofu.MockEvalContext{
		PathPath:          s.path,
		ChangesChanges:    plans.NewChanges().SyncWrapper(),
		StateState:        states.NewState().SyncWrapper(),
		RefreshStateState: states.NewState().SyncWrapper(),
		ChecksState:       checks.NewState(nil),

		// Variables
		GetVariableValueFunc: func(addr addrs.AbsInputVariableInstance) cty.Value {
			return varCache
		},
		SetModuleCallArgumentFunc: func(callAddr addrs.ModuleCallInstance, varAddr addrs.InputVariable, v cty.Value) {
			varCache = v
		},

		// Evaluation
		EvaluationScopeResultFunc: func(
			self addrs.Referenceable,
			source addrs.Referenceable,
			keyData tofu.InstanceKeyEvalData,
		) *lang.Scope {
			return &lang.Scope{
				Data: &evalData{
					caller:    caller,
					instance:  keyData,
					variables: s.variables,
					locals:    s.locals,
					resources: s.resources,
					calls:     s.calls,
					outputs:   s.outputs,
				},
				ParseRef: addrs.ParseRef,
				//SelfAddr:          self,
				//SourceAddr:        source,
				PureOnly: s.op != walkApply && s.op != walkDestroy && s.op != walkEval,
				BaseDir:  ".", // Always current working directory for now.
				//PlanTimestamp:     e.PlanTimestamp,
				//ProviderFunctions: functions,
			}
		},

		InstanceExpanderExpander: s.expander,
	}
	evalCtx.InstallSimpleEval()
	return evalCtx

}
