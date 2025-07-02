package tofu

import (
	"context"
	"fmt"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

func WalkApply(ctx context.Context, config *configs.Config, tofuCtx *Context, plan *plans.Plan) (*states.State, *checks.State, tfdiags.Diagnostics) {
	state := plan.PriorState
	if state == nil {
		state = states.NewState()
	}

	variables := InputValues{}
	{
		// From context_apply.go
		var diags tfdiags.Diagnostics
		for name, dyVal := range plan.VariableValues {
			val, err := dyVal.Decode(cty.DynamicPseudoType)
			if err != nil {
				diags = diags.Append(tfdiags.Sourceless(
					tfdiags.Error,
					"Invalid variable value in plan",
					fmt.Sprintf("Invalid value for variable %q recorded in plan file: %s.", name, err),
				))
				continue
			}

			variables[name] = &InputValue{
				Value:      val,
				SourceType: ValueFromPlan,
			}
		}
		if diags.HasErrors() {
			return nil, nil, diags
		}

		// The plan.VariableValues field only records variables that were actually
		// set by the caller in the PlanOpts, so we may need to provide
		// placeholders for any other variables that the user didn't set, in
		// which case OpenTofu will once again use the default value from the
		// configuration when we visit these variables during the graph walk.
		for name := range config.Module.Variables {
			if _, ok := variables[name]; ok {
				continue
			}
			variables[name] = &InputValue{
				Value:      cty.NilVal,
				SourceType: ValueFromPlan,
			}
		}
	}

	scope := NewRootScope(walkApply, tofuCtx, state.DeepCopy(), state.DeepCopy(), state, plan.Changes, config)

	for _, configElem := range plan.Checks.ConfigResults.Elems {
		if configElem.Value.ObjectAddrsKnown() {
			configAddr := configElem.Key
			scope.Checks.ReportCheckableObjects(configAddr, configElem.Value.ObjectResults.Keys())
		}
	}

	root := NewModule(ctx, addrs.RootModuleInstance, config, NewRootVariableInputs(variables), scope)

	p := NewManager(tofuCtx.Semaphore())
	root.Collect(p)
	edges, diags := p.Wait()
	fmt.Printf("Detected %v edges\n", len(edges))

	/*
		for _, mod := range state.Modules {
			for _, res := range mod.Resources {
				for key, inst := range res.Instances {
					addr := res.Addr.Instance(key)
					println(addr.String())
					if inst.Current.CreateBeforeDestroy {
						fmt.Printf("CBD: %s\n", addr.String())
					}
				}
			}
		}*/

	//spew.Dump(edges)
	return state, scope.Checks, diags
}

/*

type ApplyData struct {
	changes *plans.Changes
	state   *states.SyncState
	sync    func(state *states.State)
}

type ModuleApplyData struct {
	Addr addrs.ModuleInstance
	data ApplyData
}

func (m *ModuleApplyData) Child(call addrs.ModuleCallInstance) *ModuleApplyData {
	return &ModuleApplyData{
		Addr: call.Absolute(m.Addr),
		data: m.data,
	}
}

func (m *ModuleApplyData) Output(addr addrs.OutputValue) *OutputApplyData {
	return &OutputApplyData{
		Addr: addr.Absolute(m.Addr),
		data: m.data,
	}
}

type OutputApplyData struct {
	Addr addrs.AbsOutputValue
	data ApplyData
}

func (o *OutputApplyData) GetChange() {
	return
}
func (o *OutputApplyData) GetChangeAndExpected() (*states.OutputValue, *plans.OutputChangeSrc)  {
	o.changes.OutputValue(o.Addr)
	return o.state.OutputValue(o.Addr)
}
func (o *OutputApplyData) SetValue(value *states.OutputValue) {
	if o.GetValue() != value {

	}
}*/
