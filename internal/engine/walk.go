package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/checks"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/lang/evalchecks"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type Executor struct {
	cancel context.CancelFunc
	ctx    context.Context
	root   *Root

	diags chan tfdiags.Diagnostics
	jobs  sync.WaitGroup
}

func (e *Executor) Start(fn func() tfdiags.Diagnostics) {
	if e.ctx.Err() != nil {
		return
	}

	e.jobs.Add(1)
	go func() {
		e.diags <- fn()
		e.jobs.Done()
	}()
}

func (e *Executor) Wait() tfdiags.Diagnostics {
	jobWait := make(chan struct{}, 1)
	go func() {
		e.jobs.Wait()
		jobWait <- struct{}{}
	}()

	var diags tfdiags.Diagnostics
	for {
		select {
		case <-e.ctx.Done():
			// Cancelled
			return diags.Append(e.ctx.Err())
		case <-jobWait:
			// All work completed
			return diags
		case newDiags := <-e.diags:
			// New work result
			diags = diags.Append(newDiags)
			if newDiags.HasErrors() {
				e.cancel()
			}

		}
	}
}

func (r *Root) Walk() tfdiags.Diagnostics {
	ctx, cancel := context.WithCancel(context.TODO())

	exec := &Executor{
		cancel: cancel,
		ctx:    ctx,
		root:   r,
	}

	// Start expansion walk
	r.Module.visit(exec)

	return exec.Wait()
}

func (c *ModuleCalls) visit(exec *Executor) {
	exec.Start(func() tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics
		// Perform Expansion
		if c.Config != nil {
			ensure := func(key addrs.InstanceKey, data instances.RepetitionData) {
				_, ok := c.Instances[key]
				if !ok {
					instance := NewModule(c.Caller.Addr.Child(c.Config.Name, key), c)
					instance.RepetitionData = &data

					c.Instances[key] = instance
				}
			}

			switch {
			case c.Config.Count != nil:
				count, countDiags := evalchecks.EvaluateCountExpression(
					c.Config.Count,
					func(expr hcl.Expression) (cty.Value, tfdiags.Diagnostics) {
						refs, diags := lang.ReferencesInExpr(addrs.ParseRef, c.Config.Count)
						if diags.HasErrors() {
							return cty.NilVal, diags
						}
						scope, scopeDiags := c.Caller.ScopeForReferences(refs, c)
						diags = diags.Append(scopeDiags)
						if diags.HasErrors() {
							return cty.NilVal, diags
						}

						return scope.EvalExpr(expr, cty.Number)
					}, nil)

				diags = diags.Append(countDiags)

				for index := range count {
					ensure(addrs.IntKey(index), instances.RepetitionData{
						CountIndex: cty.NumberIntVal(int64(index)),
					})
				}
			case c.Config.ForEach != nil:
				forEachVals, forEachDiags := evalchecks.EvaluateForEachExpression(
					c.Config.ForEach,
					func(refs []*addrs.Reference) (*hcl.EvalContext, tfdiags.Diagnostics) {
						// TODO Eval Context / refs
						return &hcl.EvalContext{}, nil
					}, nil)

				diags = diags.Append(forEachDiags)

				for key, val := range forEachVals {
					ensure(addrs.StringKey(key), instances.RepetitionData{
						EachKey:   cty.StringVal(key),
						EachValue: val,
					})
				}
			default:
				ensure(addrs.NoKey, instances.RepetitionData{})
			}
		}

		// Visit all instances
		for _, child := range c.Instances {
			child.visit(exec)
		}

		return diags
	})
}

func (m *Module) visit(exec *Executor) {
	if m.Config != nil {
		// Ensure objects in config have been populated
		for name, call := range m.Config.ModuleCalls {
			addr := addrs.ModuleCall{Name: name}
			if _, ok := m.ModuleCalls[addr]; !ok {
				m.ModuleCalls[addr] = NewModuleCalls(m, call)
			}
			m.ModuleCalls[addr].Config = call
		}
		for name, resource := range m.Config.ManagedResources {
			addr := addrs.Resource{Name: name}
			if _, ok := m.Resources[addr]; !ok {
				m.Resources[addr] = NewResources(m, resource)
			}
			m.Resources[addr].Config = resource
		}
		for name, resource := range m.Config.DataResources {
			addr := addrs.Resource{Name: name}
			if _, ok := m.Resources[addr]; !ok {
				m.Resources[addr] = NewResources(m, resource)
			}
			m.Resources[addr].Config = resource
		}
	}

	for _, instances := range m.ModuleCalls {
		instances.visit(exec)
	}
	for _, resources := range m.Resources {
		resources.visit(exec)
	}
}

func (m *Module) ScopeForReferences(refs []*addrs.Reference, requester any) (*lang.Scope, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	data := NewEvalData()

	for _, ref := range refs {
		rawSubj := ref.Subject

		// TODO this function skips a *lot* of validation

		// This type switch must cover all of the "Referenceable" implementations
		// in package addrs, however we are removing the possibility of
		// Instances beforehand.
		// TODO we can be *much* smarter about this
		switch addr := rawSubj.(type) {
		case addrs.ResourceInstance:
			rawSubj = addr.ContainingResource()
		case addrs.ModuleCallInstance:
			rawSubj = addr.Call
		case addrs.ModuleCallInstanceOutput:
			rawSubj = addr.Call.Call
		}

		switch subj := rawSubj.(type) {
		case addrs.Resource:
			resource := m.Resources[subj]
			data.Resources[subj] = resource.Value

		case addrs.ModuleCall:
			calls := m.ModuleCalls[subj]
			data.Modules[subj] = calls.Value

		case addrs.InputVariable:
			variable := m.Variables[subj]
			data.InputVariables[subj] = variable.Value

		case addrs.LocalValue:
			local := m.Locals[subj]
			data.LocalValues[subj] = local.Value

		/* TODO
		case addrs.PathAttr:
			b.pathAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetPathAttr(subj, rng))

		case addrs.TerraformAttr:
			b.terraformAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetTerraformAttr(subj, rng))
		*/
		case addrs.CountAttr:
			data.CountAttrs[subj] = func() (cty.Value, tfdiags.Diagnostics) {
				return m.RepetitionData.CountIndex, nil
			}

		case addrs.ForEachAttr:
			data.ForEachAttrs[subj] = func() (cty.Value, tfdiags.Diagnostics) {
				switch subj.Name {
				case "key":
					return m.RepetitionData.EachKey, nil
				case "value":
					return m.RepetitionData.EachValue, nil
				default:
					panic("impossible")
				}
			}
		case addrs.OutputValue:
			output := m.Outputs[subj]
			data.Outputs[subj] = output.Value

		/* TODO
		case addrs.Check:
			b.outputValues[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetCheckBlock(subj, rng))
		*/

		default:
			// Should never happen
			panic(fmt.Errorf("Scope.buildEvalContext cannot handle address type %T", rawSubj))
		}
	}

	return &lang.Scope{
		Data:     data,
		ParseRef: addrs.ParseRef,
		//SelfAddr:          self,
		//SourceAddr:        source,
		//PureOnly:          e.Operation != walkApply && e.Operation != walkDestroy && e.Operation != walkEval,
		//BaseDir:           ".", // Always current working directory for now.
		//PlanTimestamp:     e.PlanTimestamp,
		//ProviderFunctions: functions,
	}, diags
}

func (r *Resources) visit(exec *Executor) {
	// TODO Expand Expr
}

func (v *Variable) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}

func (l *Local) Value() (cty.Value, tfdiags.Diagnostics) {
	return l.value()
}

func (l *Local) value() (cty.Value, tfdiags.Diagnostics) {
	node := &tofu.NodeLocal{
		Addr:   addrs.LocalValue{Name: l.Config.Name}.Absolute(l.Module.Addr),
		Config: l.Config,
	}

	state := states.NewState()
	evalCtx := &tofu.MockEvalContext{
		PathPath:   l.Module.Addr,
		StateState: state.SyncWrapper(),
		EvaluateExprResultFunc: func(
			expr hcl.Expression,
			wantType cty.Type,
			self addrs.Referenceable,
		) (cty.Value, tfdiags.Diagnostics) {
			refs := node.References()
			scope, diags := l.Module.ScopeForReferences(refs, l)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			value, valueDiags := scope.EvalExpr(expr, wantType)
			return value, diags.Append(valueDiags)
		},
	}

	diags := node.Execute(context.TODO(), evalCtx, tofu.WalkOperation(walkPlan))
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return state.LocalValue(node.Addr), diags
}

func (o *Output) Value() (cty.Value, tfdiags.Diagnostics) {
	return o.value()
}
func (o *Output) value() (cty.Value, tfdiags.Diagnostics) {
	// TODO NodeDestroyableOutput
	node := &tofu.NodeApplyableOutput{
		Addr:   addrs.OutputValue{Name: o.Config.Name}.Absolute(o.Module.Addr),
		Config: o.Config,
		//Change:       change,
		//RefreshOnly:  o.RefreshOnly,
		//DestroyApply: o.Destroying,
		//Planning:     o.Planning,
	}

	state := states.NewState()
	evalCtx := &tofu.MockEvalContext{
		PathPath:          o.Module.Addr,
		StateState:        state.SyncWrapper(),
		RefreshStateState: state.SyncWrapper(),
		ChecksState:       checks.NewState(nil),
		// Depends-on EvaluationScope
	}

	diags := node.Execute(context.TODO(), evalCtx, tofu.WalkOperation(walkPlan))
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return state.OutputValue(node.Addr).Value, diags
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

func (r *Resources) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}
