package engine

import (
	"log"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/plugins"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type Scope struct {
	path     addrs.ModuleInstance
	op       WalkOperation
	expander *instances.Expander
	hooks    []tofu.Hook
	Plugins  plugins.Manager

	altData func(caller promise, instance instances.RepetitionData) lang.Data

	variables map[addrs.InputVariable]*Promise[cty.Value]
	locals    map[addrs.LocalValue]*Promise[cty.Value]
	resources map[addrs.Resource]*Promise[cty.Value]
	calls     map[addrs.ModuleCall]*Promise[cty.Value]
	outputs   map[addrs.OutputValue]*Promise[cty.Value]
}

func NewRootScope(op WalkOperation, pluginManager plugins.Manager, hooks []tofu.Hook) *Scope {
	return &Scope{
		path:     addrs.RootModuleInstance,
		op:       op,
		expander: instances.NewExpander(),
		hooks:    hooks,
		Plugins:  pluginManager,

		variables: map[addrs.InputVariable]*Promise[cty.Value]{},
		locals:    map[addrs.LocalValue]*Promise[cty.Value]{},
		resources: map[addrs.Resource]*Promise[cty.Value]{},
		calls:     map[addrs.ModuleCall]*Promise[cty.Value]{},
		outputs:   map[addrs.OutputValue]*Promise[cty.Value]{},
	}
}

func NewScope(path addrs.ModuleInstance, parent *Scope) *Scope {
	return &Scope{
		path:     path,
		op:       parent.op,
		expander: parent.expander,
		hooks:    parent.hooks,
		Plugins:  parent.Plugins,

		variables: map[addrs.InputVariable]*Promise[cty.Value]{},
		locals:    map[addrs.LocalValue]*Promise[cty.Value]{},
		resources: map[addrs.Resource]*Promise[cty.Value]{},
		calls:     map[addrs.ModuleCall]*Promise[cty.Value]{},
		outputs:   map[addrs.OutputValue]*Promise[cty.Value]{},
	}
}

func NewScopeAlt[Variable, Local, Resource, Call, Output ValuePromise](path addrs.ModuleInstance, parent *Scope, data ModuleData[Variable, Local, Resource, Call, Output]) *Scope {
	return &Scope{
		path:     path,
		op:       parent.op,
		expander: parent.expander,
		hooks:    parent.hooks,
		Plugins:  parent.Plugins,

		altData: func(caller promise, instance instances.RepetitionData) lang.Data {
			return &evalDataAlt[Variable, Local, Resource, Call, Output]{
				caller,
				instance,
				data,
			}
		},
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
		PrevRunStateState: states.NewState().SyncWrapper(),
		ChecksState:       checks.NewState(nil),
		HookFn: func(fn func(tofu.Hook) (tofu.HookAction, error)) error {
			// Lifted from BuiltinEvalContext
			for _, h := range s.hooks {
				action, err := fn(h)
				if err != nil {
					return err
				}

				switch action {
				case tofu.HookActionContinue:
					continue
				case tofu.HookActionHalt:
					// Return an early exit error to trigger an early exit
					log.Printf("[WARN] Early exit triggered by hook: %T", h)
					return nil
				}
			}

			return nil
		},

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
			var data lang.Data
			if s.altData != nil {
				data = s.altData(caller, keyData)
			} else {
				data = &evalData{
					caller:    caller,
					instance:  keyData,
					variables: s.variables,
					locals:    s.locals,
					resources: s.resources,
					calls:     s.calls,
					outputs:   s.outputs,
				}
			}
			return &lang.Scope{
				Data:     data,
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
