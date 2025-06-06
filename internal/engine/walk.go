package engine

import (
	"context"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/lang/evalchecks"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

// This is wired together a bit odd.  Instead of promising diagnostics as the "everything is done" value, it should instead return changes + state for each item
type WalkData struct {
	Context context.Context
	Cancel  context.CancelFunc
	Op      WalkOperation

	Config       *configs.Config
	InputState   *states.SyncState
	InputChanges *plans.ChangesSync
	InputVars    map[addrs.InputVariable]*Promise[cty.Value]
}

func Walk(data *WalkData) (*plans.Changes, *states.State, tfdiags.Diagnostics) {
	root, diags := Module(data, addrs.RootModuleInstance, data.Config, &tofu.MockEvalContext{
		// Only used to hack in the expander
		InstanceExpanderExpander: instances.NewExpander(),
	}, nil)

	//checks := checks.NewState(data.Config)
	change := plans.NewChanges()
	state := states.NewState()
	// Flatten results to "standard" format
	recordDiags := root.Recorder(change.SyncWrapper(), state.SyncWrapper())

	return change, state, diags.Append(recordDiags)
}

type Recorder func(*plans.ChangesSync, *states.SyncState) tfdiags.Diagnostics

type ModuleCallValue struct {
	Expanded *Promise[cty.Value]
	Recorder Recorder
}

type VariableInput struct {
	expr hcl.Expression
}

func ModuleCall(data *WalkData, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, evalCtx tofu.EvalContext) (ModuleCallValue, tfdiags.Diagnostics) {
	// Lifted from transform_module_variable
	// We need to construct a schema for the expected call arguments based on
	// the configured variables in our config, which we can then use to
	// decode the content of the call block.
	schema := &hcl.BodySchema{}
	for _, v := range moduleConfig.Module.Variables {
		schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{
			Name:     v.Name,
			Required: v.Default == cty.NilVal,
		})
	}

	content, contentDiags := config.Config.Content(schema)
	diags := tfdiags.Diagnostics{}.Append(contentDiags)
	if diags.HasErrors() {
		// Validation code elsewhere should deal with any errors before we
		// get in here, but we'll report them out here just in case, to
		// avoid crashes.
		return ModuleCallValue{}, diags
	}

	// Legacy expander integration.  We should just be passing around RepetitionData instead.
	expander := evalCtx.InstanceExpander()

	childInstances := map[addrs.InstanceKey]ModuleValue{}

	ensure := func(key addrs.InstanceKey, repetitionData instances.RepetitionData) {
		input := map[addrs.InputVariable]VariableInput{}

		for _, v := range moduleConfig.Module.Variables {
			var expr hcl.Expression
			if attr := content.Attributes[v.Name]; attr != nil {
				expr = attr.Expr
			}
			input[addrs.InputVariable{Name: v.Name}] = VariableInput{expr: expr}
		}

		childInstance, childDiags := Module(data, addr.Instance(key), moduleConfig, evalCtx, input)
		childInstances[key] = childInstance
		diags = diags.Append(childDiags)
	}

	switch {
	case config.Count != nil:
		count, countDiags := evalchecks.EvaluateCountExpression(
			config.Count,
			func(expr hcl.Expression) (cty.Value, tfdiags.Diagnostics) {
				return evalCtx.EvaluateExpr(expr, cty.Number, nil)
			},
			nil,
		)

		diags = diags.Append(countDiags)
		expander.SetModuleCount(addr.Module, addr.Call, count)

		for index := range count {
			ensure(addrs.IntKey(index), instances.RepetitionData{
				CountIndex: cty.NumberIntVal(int64(index)),
			})
		}
	case config.ForEach != nil:
		forEachVals, forEachDiags := evalchecks.EvaluateForEachExpression(
			config.ForEach,
			func(refs []*addrs.Reference) (*hcl.EvalContext, tfdiags.Diagnostics) {
				scope := evalCtx.EvaluationScope(nil, nil, tofu.InstanceKeyEvalData{})
				return scope.EvalContext(refs)
			}, nil)

		diags = diags.Append(forEachDiags)

		for key, val := range forEachVals {
			ensure(addrs.StringKey(key), instances.RepetitionData{
				EachKey:   cty.StringVal(key),
				EachValue: val,
			})
		}
	default:
		expander.SetModuleSingle(addr.Module, addr.Call)
		ensure(addrs.NoKey, instances.RepetitionData{})
	}

	type Hack struct{ addrs.AbsModuleCall }
	key := Hack{addr}

	var expanded *Promise[cty.Value]
	expanded = NewPromise[cty.Value](key, func() (cty.Value, tfdiags.Diagnostics) {
		moduleInstances := make(map[addrs.InstanceKey]cty.Value)
		for key, mod := range childInstances {
			var modDiags tfdiags.Diagnostics
			moduleInstances[key], modDiags = mod.Outputs.Value(expanded)
			diags = diags.Append(modDiags)
		}
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		// Lifted from tofu/evaluate.go

		switch {
		case config.Count != nil:
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

		case config.ForEach != nil:
			instanceMap := make(map[string]cty.Value)
			for key, mod := range moduleInstances {
				sk := key.(addrs.StringKey)
				instanceMap[string(sk)] = mod
			}
			return cty.ObjectVal(instanceMap), diags
		default:
			return moduleInstances[addrs.NoKey], diags
		}
	})

	record := func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics
		var diagLock sync.Mutex
		var wg sync.WaitGroup

		for _, inst := range childInstances {
			inst := inst

			wg.Add(1)
			go func() {
				defer wg.Done()
				instDiags := inst.Recorder(change, state)
				diagLock.Lock()
				diags = diags.Append(instDiags)
				diagLock.Unlock()
			}()
		}

		wg.Wait()

		return diags
	}

	return ModuleCallValue{
		Expanded: expanded,
		Recorder: record,
	}, diags
}

type ModuleValue struct {
	Outputs  *Promise[cty.Value]
	Recorder Recorder
}

func Module(data *WalkData, addr addrs.ModuleInstance, config *configs.Config, parentEvalCtx tofu.EvalContext, input map[addrs.InputVariable]VariableInput) (ModuleValue, tfdiags.Diagnostics) {
	variables := map[addrs.InputVariable]*Promise[cty.Value]{}
	locals := map[addrs.LocalValue]*Promise[cty.Value]{}
	resources := map[addrs.Resource]*Promise[cty.Value]{}
	calls := map[addrs.ModuleCall]*Promise[ModuleCallValue]{}
	outputs := map[addrs.OutputValue]*Promise[OutputValue]{}

	if config != nil {
		// Legacy expander integration.  We should just be passing around RepetitionData instead.
		//repetitionData := parentEvalCtx.InstanceExpander().GetModuleInstanceRepetitionData(addr)

		scopeForCaller := func(caller promise, instance instances.RepetitionData) *lang.Scope {
			return &lang.Scope{
				Data: &evalData{
					caller:    caller,
					instance:  instance,
					variables: variables,
					locals:    locals,
					resources: resources,
					calls:     calls,
					outputs:   outputs,
				},
				ParseRef: addrs.ParseRef,
				//SelfAddr:          self,
				//SourceAddr:        source,
				PureOnly: data.Op != walkApply && data.Op != walkDestroy && data.Op != walkEval,
				BaseDir:  ".", // Always current working directory for now.
				//PlanTimestamp:     e.PlanTimestamp,
				//ProviderFunctions: functions,
			}
		}

		evalContextFor := func(caller promise) tofu.EvalContext {
			// I think this can be stupid?
			// This is just a hack for the variable input passthrough from parent -> child in the variable nodes
			var varCache cty.Value

			evalCtx := &tofu.MockEvalContext{
				PathPath:          addr,
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
					return scopeForCaller(caller, keyData)
				},

				InstanceExpanderExpander: parentEvalCtx.InstanceExpander(),
			}
			evalCtx.InstallSimpleEval()
			return evalCtx
		}

		for _, variable := range config.Module.Variables {
			variable := variable

			varAddr := addrs.InputVariable{Name: variable.Name}
			varAddrAbs := varAddr.Absolute(addr)

			variables[varAddr] = NewPromise[cty.Value](varAddrAbs, func() (cty.Value, tfdiags.Diagnostics) {
				return Variable(data, varAddrAbs, variable, evalContextFor(variables[varAddr]), input[varAddr], parentEvalCtx)
			})
		}
		for _, local := range config.Module.Locals {
			local := local

			localAddr := addrs.LocalValue{Name: local.Name}
			localAddrAbs := localAddr.Absolute(addr)

			// TODO Local Values in the state (for tofu console)
			locals[localAddr] = NewPromise[cty.Value](localAddrAbs, func() (cty.Value, tfdiags.Diagnostics) {
				return Local(data, localAddrAbs, local, evalContextFor(locals[localAddr]))
			})
		}
		for _, resource := range config.Module.ManagedResources {
			resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
			resources[resAddr] = nil // TODO
		}
		// TODO DataResources
		for _, call := range config.Module.ModuleCalls {
			call := call
			callAddr := addrs.ModuleCall{Name: call.Name}
			callAddrAbs := callAddr.Absolute(addr)

			calls[callAddr] = NewPromise[ModuleCallValue](callAddrAbs, func() (ModuleCallValue, tfdiags.Diagnostics) {
				return ModuleCall(data, callAddrAbs, call, config.Children[call.Name], evalContextFor(calls[callAddr]))
			})
		}
		for _, output := range config.Module.Outputs {
			output := output

			outputAddr := addrs.OutputValue{Name: output.Name}
			outputAddrAbs := outputAddr.Absolute(addr)

			outputs[outputAddr] = NewPromise[OutputValue](outputAddrAbs, func() (OutputValue, tfdiags.Diagnostics) {
				return Output(
					data, outputAddrAbs, output,
					data.InputChanges.GetOutputChange(outputAddrAbs),
					evalContextFor(outputs[outputAddr]),
				)
			})
		}
	}
	//TODO  data.InputState / data.InputChanges

	// We have fully built all things we are responsible for and can now return:
	// - Outputs that can be queried at will
	// - Translation to the legacy change and state formats

	var outputValue *Promise[cty.Value]
	outputValue = NewPromise(addr, func() (cty.Value, tfdiags.Diagnostics) {
		obj := map[string]cty.Value{}
		var diags tfdiags.Diagnostics
		for name := range config.Module.Outputs {
			outVal, outDiags := outputs[addrs.OutputValue{Name: name}].Value(outputValue)
			obj[name] = outVal.state.Value
			diags = diags.Append(outDiags)
		}
		return cty.ObjectVal(obj), diags
	})

	record := func(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics

		var wg sync.WaitGroup
		var diagLock sync.Mutex

		for outAddr, output := range outputs {
			outAddr := outAddr
			output := output
			wg.Add(1)

			go func() {
				defer wg.Done()
				out, outDiags := output.Value(nil)

				diagLock.Lock()
				diags = diags.Append(outDiags)
				diagLock.Unlock()

				if outDiags.HasErrors() {
					return
				}
				if out.state != nil {
					state.SetOutputValue(outAddr.Absolute(addr), out.state.Value, out.state.Sensitive, out.state.Deprecated)
				}
				if out.change != nil {
					change.AppendOutputChange(out.change)
				}
			}()
		}

		for _, call := range calls {
			call := call
			wg.Add(1)
			go func() {
				defer wg.Done()

				cv, cdiags := call.Value(nil)
				diagLock.Lock()
				diags = diags.Append(cdiags)
				if cv.Recorder != nil {
					diags = diags.Append(cv.Recorder(change, state))
				}
				diagLock.Unlock()
			}()
		}

		wg.Wait()

		return diags
	}

	return ModuleValue{outputValue, record}, nil
}

func Variable(data *WalkData, addr addrs.AbsInputVariableInstance, config *configs.Variable, evalCtx tofu.EvalContext, expr hcl.Expression, parentEvalCtx tofu.EvalContext) (cty.Value, tfdiags.Diagnostics) {
	// It is evaluated in the "parent" module
	input := &tofu.NodeModuleVariable{
		Addr:           addr,
		Config:         config,
		Expr:           expr,
		ModuleInstance: addr.Module,
	}
	diags := input.Execute(data.Context, parentEvalCtx, tofu.WalkOperation(data.Op))
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	// HACK: Shift value from parent to child context (see mock hack)
	_, call := addr.Module.CallInstance()
	evalCtx.SetModuleCallArgument(
		call, addr.Variable,
		parentEvalCtx.GetVariableValue(addr),
	)

	// It is evaluated in the "child" module
	ref := &tofu.NodeVariableReferenceInstance{
		Addr:   addr,
		Config: config,
		Expr:   expr,

		// TODO VariableFromRemoteModule: c.IsModuleCallFromRemoteModule(callConfig.Name),
	}
	execDiags := ref.Execute(data.Context, evalCtx, tofu.WalkOperation(data.Op))
	diags = diags.Append(execDiags)
	return parentEvalCtx.GetVariableValue(addr), diags
}

func Local(data *WalkData, addr addrs.AbsLocalValue, config *configs.Local, evalCtx tofu.EvalContext) (cty.Value, tfdiags.Diagnostics) {
	node := &tofu.NodeLocal{
		Addr:   addr,
		Config: config,
	}
	diags := node.Execute(data.Context, evalCtx, tofu.WalkOperation(data.Op))
	return evalCtx.State().LocalValue(addr), diags
}

type OutputValue struct {
	state  *states.OutputValue
	change *plans.OutputChangeSrc
}

func Output(data *WalkData, addr addrs.AbsOutputValue, config *configs.Output, change *plans.OutputChangeSrc, evalCtx tofu.EvalContext) (OutputValue, tfdiags.Diagnostics) {
	// TODO NodeDestroyableOutput
	node := &tofu.NodeApplyableOutput{
		Addr:   addr,
		Config: config,
		Change: change,
		//TODO RefreshOnly:  o.RefreshOnly,
		DestroyApply: data.Op == walkDestroy || data.Op == walkPlanDestroy,
		Planning:     true, // Always true in the rest of the code base
	}
	diags := node.Execute(data.Context, evalCtx, tofu.WalkOperation(data.Op))

	return OutputValue{
		evalCtx.State().OutputValue(node.Addr),
		evalCtx.Changes().GetOutputChange(addr),
	}, diags
}
