package engine

import (
	"context"
	"log"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/configs/configschema"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/plugins"
	"github.com/opentofu/opentofu/internal/providers"
	"github.com/opentofu/opentofu/internal/provisioners"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
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

	Checks *checks.State

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
	cfg *configs.Config,
) *Scope {
	return &Scope{
		path:      addrs.RootModuleInstance,
		op:        op,
		expander:  instances.NewExpander(),
		hooks:     hooks,
		Plugins:   pluginManager,
		workspace: workspace,

		Checks: checks.NewState(cfg),

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

		Checks: parent.Checks,

		PrevRun: parent.PrevRun,
		Refresh: parent.Refresh,
		State:   parent.State,
		Changes: parent.Changes,

		Data: data,
	}
}

func (s *Scope) EvalContext(caller executor, overrides ...DataOverride) tofu.EvalContext {
	// I think this can be stupid?
	// This is just a hack for the variable input passthrough from parent -> child in the variable nodes
	var varCache cty.Value

	evalCtx := &tofu.MockEvalContext{
		PathPath:          s.path,
		ChangesChanges:    s.Changes,
		StateState:        s.State,
		RefreshStateState: s.Refresh,
		PrevRunStateState: s.PrevRun,
		ChecksState:       s.Checks,
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

		// Providers
		ProviderFn: func(addr addrs.AbsProviderConfig, _ addrs.InstanceKey) providers.Interface {
			// TODO keep mapping to parent providers in-tact
			// hack for now
			providerConfig := s.Data.Providers[addrs.LocalProviderConfig{
				LocalName: addr.Provider.Type,
				Alias:     addr.Alias,
			}]

			// TODO manage scope of provider (if we care)
			provider, _, _ := providerConfig(caller)
			return provider
		},
		ProviderSchemaFn: func(_ context.Context, addr addrs.AbsProviderConfig) (providers.ProviderSchema, error) {
			return s.Plugins.ProviderSchema(addr.Provider)
		},

		// Provisioners
		ProvisionerFn: func(n string) (provisioners.Interface, error) {
			return s.Plugins.NewProvisionerInstance(n)
		},
		ProvisionerSchemaFn: func(n string) (*configschema.Block, error) {
			return s.Plugins.ProvisionerSchema(n)
		},

		// Variables
		GetVariableValueFunc: func(addr addrs.AbsInputVariableInstance) cty.Value {
			return varCache
		},
		SetModuleCallArgumentFunc: func(callAddr addrs.ModuleCallInstance, varAddr addrs.InputVariable, v cty.Value) {
			varCache = v
		},
		SetRootModuleArgumentFunc: func(varAddr addrs.InputVariable, v cty.Value) {
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
					overrides,
					s.Data,
				},
				ParseRef:   addrs.ParseRef,
				SelfAddr:   self,
				SourceAddr: source,
				PureOnly:   s.op != walkApply && s.op != walkDestroy && s.op != walkEval,
				BaseDir:    ".", // Always current working directory for now.
				//PlanTimestamp:     e.PlanTimestamp,
				ProviderFunctions: func(pf addrs.ProviderFunction, rng tfdiags.SourceRange) (*function.Function, tfdiags.Diagnostics) {
					providerConfig := s.Data.Providers[addrs.LocalProviderConfig{
						LocalName: pf.ProviderName,
						Alias:     pf.ProviderAlias,
					}]

					// TODO manage scope of provider (if we care)
					provider, _, _ := providerConfig(caller)

					return tofu.EvalContextProviderFunction(provider, tofu.WalkOperation(s.op), pf, rng)
				},
			}
		},

		InstanceExpanderExpander: s.expander,
	}
	evalCtx.InstallSimpleEval()
	return evalCtx

}
