package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/instances"
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

	// Start unexpanded walk
	r.ModuleCall.visit(exec)
	// Start expansion walk
	r.ModuleCallInstance.visit(exec)

	return exec.Wait()
}

func (c *ModuleCall) visit(exec *Executor) {
	fmt.Printf("Starting Call Visit %s\n", c.Addr)

	c.Status = StatusPending
	c.Dependents = nil

	c.Module.visit(exec)
}

func (m *Module) visit(exec *Executor) {
	// Visit Children
	for _, call := range m.Calls {
		call.visit(exec)
	}

	for _, resource := range m.Resources {
		resource.visit(exec)
	}
}

func (r *Resource) visit(exec *Executor) {
	// Setup State
}

func (c *ModuleCallInstances) visit(exec *Executor) {
	exec.Start(func() tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics
		// Perform Expansion
		if c.countDiags.Config != nil {
			ensureInstance := func(key addrs.InstanceKey, data instances.RepetitionData) {
				// Ensure singleton
				_, ok := c.Instances[key]
				if !ok {
					mod := NewModuleInstance(c.ParentAddr.Child(c.countDiags.Config.Name, key), c.countDiags.Module)

					instance := NewModuleCallInstance(mod)
					instance.RepetitionData = data

					c.Instances[key] = instance
				}
			}

			switch {
			case c.countDiags.Config.Count != nil:
				count, countDiags := evalchecks.EvaluateCountExpression(
					c.countDiags.Config.Count,
					func(expr hcl.Expression) (cty.Value, tfdiags.Diagnostics) {
						// TODO Eval Context / refs
						val, diags := expr.Value(nil)
						return val, tfdiags.Diagnostics(nil).Append(diags)
					}, nil)

				diags = diags.Append(countDiags)

				for index := range count {
					ensureInstance(addrs.IntKey(index), instances.RepetitionData{
						CountIndex: cty.NumberIntVal(int64(index)),
					})
				}
			case c.countDiags.Config.ForEach != nil:
				forEachVals, forEachDiags := evalchecks.EvaluateForEachExpression(
					c.countDiags.Config.ForEach,
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

func (c *ModuleCallInstance) visit(exec *Executor) {
	// TODO resolve variable values
	c.ModuleInstance.visit(exec)
}

func (m *ModuleInstance) visit(exec *Executor) {
	// Ensure all the calls exist
	for name, call := range m.Module.Calls {
		if _, ok := m.Calls[name]; !ok {
			m.Calls[name] = NewModuleCallInstances(m.Addr, call)
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

func (r *ResourceInstance) value(walker *Walker) (cty.Value, tfdiags.Diagnostics) {
	//
}*/
