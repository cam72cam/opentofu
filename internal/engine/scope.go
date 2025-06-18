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
	path      addrs.ModuleInstance
	op        WalkOperation
	expander  *instances.Expander
	hooks     []tofu.Hook
	Plugins   plugins.Manager
	workspace string

	PrevRun *states.SyncState
	Refresh *states.SyncState
	State   *states.SyncState
	Changes *plans.ChangesSync

	Data ModuleData
}

func NewRootScope(
	op WalkOperation,
	pluginManager plugins.Manager,
	hooks []tofu.Hook,
	workspace string,
	prevRun *states.SyncState,
	refresh *states.SyncState,
	state *states.SyncState,
	changes *plans.ChangesSync,
) *Scope {
	return &Scope{
		path:      addrs.RootModuleInstance,
		op:        op,
		expander:  instances.NewExpander(),
		hooks:     hooks,
		Plugins:   pluginManager,
		workspace: workspace,

		PrevRun: prevRun,
		Refresh: refresh,
		State:   state,
		Changes: changes,
	}
}

func NewScope(path addrs.ModuleInstance, parent *Scope, data ModuleData) *Scope {
	return &Scope{
		path:      path,
		op:        parent.op,
		expander:  parent.expander,
		hooks:     parent.hooks,
		Plugins:   parent.Plugins,
		workspace: parent.workspace,

		PrevRun: parent.PrevRun,
		Refresh: parent.Refresh,
		State:   parent.State,
		Changes: parent.Changes,

		Data: data,
	}
}

func (s *Scope) EvalContext(caller executor) tofu.EvalContext {
	// I think this can be stupid?
	// This is just a hack for the variable input passthrough from parent -> child in the variable nodes
	var varCache cty.Value

	evalCtx := &tofu.MockEvalContext{
		PathPath:          s.path,
		ChangesChanges:    s.Changes,
		StateState:        s.State,
		RefreshStateState: s.Refresh,
		PrevRunStateState: s.PrevRun,
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
			return &lang.Scope{
				Data: &evalData{
					caller,
					keyData,
					s.workspace,
					s.Data,
				},
				ParseRef:   addrs.ParseRef,
				SelfAddr:   self,
				SourceAddr: source,
				PureOnly:   s.op != walkApply && s.op != walkDestroy && s.op != walkEval,
				BaseDir:    ".", // Always current working directory for now.
				//PlanTimestamp:     e.PlanTimestamp,
				//ProviderFunctions: functions,
			}
		},

		InstanceExpanderExpander: s.expander,
	}
	evalCtx.InstallSimpleEval()
	return evalCtx

}
