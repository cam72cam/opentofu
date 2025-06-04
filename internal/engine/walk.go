package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/lang"
	"github.com/opentofu/opentofu/internal/lang/evalchecks"
	"github.com/opentofu/opentofu/internal/tfdiags"
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
	r.ModuleInstance.visit(exec)

	return exec.Wait()
}

func (c *ModuleCallInstances) visit(exec *Executor) {
	exec.Start(func() tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics
		// Perform Expansion
		if c.ModuleCall.Config != nil {
			ensureInstance := func(key addrs.InstanceKey, data instances.RepetitionData) {
				// Ensure singleton
				_, ok := c.Instances[key]
				if !ok {
					instance := NewModuleInstance(c.Caller.Addr.Child(c.ModuleCall.Config.Name, key), c.ModuleCall.Module, c)
					instance.RepetitionData = data

					c.Instances[key] = instance
				}
			}

			switch {
			case c.ModuleCall.Config.Count != nil:
				count, countDiags := evalchecks.EvaluateCountExpression(
					c.ModuleCall.Config.Count,
					func(expr hcl.Expression) (cty.Value, tfdiags.Diagnostics) {
						refs, diags := lang.ReferencesInExpr(addrs.ParseRef, c.ModuleCall.Config.Count)
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
					ensureInstance(addrs.IntKey(index), instances.RepetitionData{
						CountIndex: cty.NumberIntVal(int64(index)),
					})
				}
			case c.ModuleCall.Config.ForEach != nil:
				forEachVals, forEachDiags := evalchecks.EvaluateForEachExpression(
					c.ModuleCall.Config.ForEach,
					func(refs []*addrs.Reference) (*hcl.EvalContext, tfdiags.Diagnostics) {
						// TODO Eval Context / refs
						return &hcl.EvalContext{}, nil
					}, nil)

				diags = diags.Append(forEachDiags)

				for key, val := range forEachVals {
					ensureInstance(addrs.StringKey(key), instances.RepetitionData{
						EachKey:   cty.StringVal(key),
						EachValue: val,
					})
				}
			default:
				ensureInstance(addrs.NoKey, instances.RepetitionData{})
			}
		}

		// Visit all instances
		for _, child := range c.Instances {
			child.visit(exec)
		}

		return diags
	})
}

func (m *ModuleInstance) visit(exec *Executor) {
	// Ensure all the calls exist
	for name, call := range m.Module.Calls {
		if _, ok := m.Calls[name]; !ok {
			m.Calls[name] = NewModuleCallInstances(m, call)
		}
	}

	// Seperate loop to enclude orphans
	for _, instances := range m.Calls {
		instances.visit(exec)
	}

	// Ensure all of the resources exist
	for name, _ := range m.Module.Resources {
		if _, ok := m.Resources[name]; !ok {
			m.Resources[name] = ResourceInstances{}
		}
	}

	// Seperate loop to include orphans
	for _, resources := range m.Resources {
		resources.visit(exec)
	}
}

func (m *ModuleInstance) ScopeForReferences(refs []*addrs.Reference, requester any) (*lang.Scope, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	data := NewEvalData()

	for _, ref := range refs {
		rawSubj := ref.Subject
		rng := ref.SourceRange

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
			calls := m.Calls[subj]
			data.Modules[subj] = calls.Value

		case addrs.InputVariable:
			variable := m.Variables[subj]
			data.InputVariables[subj] = variable.Value

		case addrs.LocalValue:
			local := m.Locals[subj]
			data.LocalValues[sobj] = local.Value

		/* TODO
		case addrs.PathAttr:
			b.pathAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetPathAttr(subj, rng))

		case addrs.TerraformAttr:
			b.terraformAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetTerraformAttr(subj, rng))
		*/
		case addrs.CountAttr:
			b.countAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetCountAttr(subj, rng))

		case addrs.ForEachAttr:
			b.forEachAttrs[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetForEachAttr(subj, rng))

		case addrs.OutputValue:
			b.outputValues[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetOutput(subj, rng))

		case addrs.Check:
			b.outputValues[subj.Name], normDiags = normalizeRefValue(b.s.Data.GetCheckBlock(subj, rng))

		default:
			// Should never happen
			panic(fmt.Errorf("Scope.buildEvalContext cannot handle address type %T", rawSubj))
		}
	}

	return nil, diags
}

func (r *ResourceInstances) visit(exec *Executor) {
	// TODO Expand Expr
}

/*
func (c *ModuleCallInstance) visit() tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	fmt.Printf("Starting Call Instance Visit %s\n", c.ModuleInstance.Addr)

	for resName, resources := range c.ModuleInstance.Resources {
		fmt.Printf("Visit resource instances %s\n", resName)
		// TODO Require expansion
		for _, resource := range resources {
			diags = diags.Append(resource.visit())
		}
	}

	return diags
}

func (r *ResourceInstance) visit() tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	fmt.Printf("Visit resource instance: %s\n", r.Addr)
	return diags
}
*/

func (v *VariableInstance) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}

func (m *ModuleCallInstances) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}

func (r *ResourceInstances) Value() (cty.Value, tfdiags.Diagnostics) {
	return cty.NilVal, nil
}
