package engine

import (
	"context"
	"fmt"
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
	op       WalkOperation
	expander *instances.Expander
	tofuCtx  *tofu.Context

	PrevRun *states.SyncState
	Refresh *states.SyncState
	State   *states.SyncState
	Changes *plans.ChangesSync

	Checks *checks.State

	Data ModuleData
}

func NewRootScope(
	op WalkOperation,
	tofuCtx *tofu.Context,
	prevRun *states.SyncState,
	refresh *states.SyncState,
	state *states.SyncState,
	changes *plans.ChangesSync,
	cfg *configs.Config,
) *Scope {
	return &Scope{
		op:       op,
		expander: instances.NewExpander(),

		tofuCtx: tofuCtx,

		Checks: checks.NewState(cfg),

		PrevRun: prevRun,
		Refresh: refresh,
		State:   state,
		Changes: changes,
	}
}

func NewScope(path addrs.ModuleInstance, parent *Scope, data ModuleData) *Scope {
	return &Scope{
		op:       parent.op,
		expander: parent.expander,
		tofuCtx:  parent.tofuCtx,

		Checks: parent.Checks,

		PrevRun: parent.PrevRun,
		Refresh: parent.Refresh,
		State:   parent.State,
		Changes: parent.Changes,

		Data: data,
	}
}

func (s *Scope) Plugins() plugins.Manager {
	return s.tofuCtx.Schemas().(plugins.Manager)
}

// From tofu
/*type graphNodeAttachDataResourceDependsOn interface {
	AttachDataResourceDependsOn(deps []addrs.ConfigResource, force bool)
}*/

func (s *Scope) LegacyExecute(ctx context.Context, caller *Executor, node tofu.GraphNodeExecutable) (tofu.EvalContext, tfdiags.Diagnostics) {
	evalCtx := s.EvalContext(caller)

	var refs []*addrs.Reference
	// TODO this is a bit of a nasty patch, everything through here should be referencer
	if gnr, ok := node.(tofu.GraphNodeReferencer); ok {
		refs = gnr.References()

		scope := evalCtx.EvaluationScope(nil, nil, tofu.EvalDataForNoInstanceKey)
		var filtered []*addrs.Reference
		for _, ref := range refs {
			switch ref.Subject.(type) {
			case // Skip non-promise references
				addrs.ForEachAttr,
				addrs.CountAttr,
				addrs.TerraformAttr,
				addrs.PathAttr:
				continue
			}
			filtered = append(filtered, ref)
		}
		_, diags := scope.EvalContext(refs)
		if diags.HasErrors() {
			return nil, diags
		}

		// This is similar to
		// TODO graphNodeAttachDataResourceDependsOn
		if gnad, ok := node.(tofu.GraphNodeAttachDependencies); ok {
			// Find dependencies to attach
			visited := caller.pool.Ancestors(caller.caller)

			var resources []addrs.ConfigResource
			for _, raw := range visited {
				// Hack for now
				type addrable interface {
					Addr() fmt.Stringer
				}
				raw = raw.(addrable).Addr()

				var addr addrs.ConfigResource
				switch v := raw.(type) {
				case addrs.AbsResourceInstance:
					addr = v.ContainingResource().Config()
				case addrs.ConfigResource:
					addr = v
				default:
					//fmt.Printf("%T = %s\n", v, v)
					continue
				}

				// TODO self addr check

				// TODO dedup
				resources = append(resources, addr)
			}

			//fmt.Printf("%s: %v\n", caller.caller, resources)
			gnad.AttachDependencies(resources)
		}
	}

	diags := node.Execute(ctx, evalCtx, tofu.WalkOperation(s.op))
	return evalCtx, diags
}

func (s *Scope) EvalContext(caller *Executor, overrides ...DataOverride) tofu.EvalContext {
	// I think this can be stupid?
	// This is just a hack for the variable input passthrough from parent -> child in the variable nodes
	var varCache cty.Value

	evalCtx := &tofu.MockEvalContext{
		PathPath:          s.Data.Addr,
		ChangesChanges:    s.Changes,
		StateState:        s.State,
		RefreshStateState: s.Refresh,
		PrevRunStateState: s.PrevRun,
		ChecksState:       s.Checks,
		HookFn: func(fn func(tofu.Hook) (tofu.HookAction, error)) error {
			// Lifted from BuiltinEvalContext
			for _, h := range s.tofuCtx.Hooks() {
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
			return s.Plugins().ProviderSchema(addr.Provider)
		},

		// Provisioners
		ProvisionerFn: func(n string) (provisioners.Interface, error) {
			return s.Plugins().NewProvisionerInstance(n)
		},
		ProvisionerSchemaFn: func(n string) (*configschema.Block, error) {
			return s.Plugins().ProvisionerSchema(n)
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
					s.tofuCtx.Workspace(),
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
