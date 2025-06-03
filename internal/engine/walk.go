package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

type Executor struct {
	cancel context.CancelFunc
	ctx    context.Context
	root   *ModuleCall

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

func (c *ModuleCall) Walk() tfdiags.Diagnostics {
	ctx, cancel := context.WithCancel(context.TODO())

	exec := &Executor{
		cancel: cancel,
		ctx:    ctx,
		root:   c,
	}
	c.visit(exec)
	return exec.Wait()
}

func (c *ModuleCall) visit(exec *Executor) {
	fmt.Printf("Starting Call Visit %s\n", c.Addr)

	c.Status = StatusPending
	c.Dependents = nil

	c.Module.visit(exec)

	if c.Parent == nil {
		// Begin Expansion from Root Module
		exec.Start(func() tfdiags.Diagnostics {
			// Expansion Path
			instances, ok := c.InstancesByPath[""]
			if !ok {
				instances = ModuleCallInstances{}
				c.InstancesByPath[""] = instances
			}
			callInstance, ok := instances[addrs.NoKey]
			if !ok {
				callInstance = &ModuleCallInstance{
					ModuleInstance: &ModuleInstance{},
					//RepetitionData instances.RepetitionData
				}
				instances[addrs.NoKey] = callInstance
			}

			callInstance.visit(exec)

			return nil
		})
	}
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
	// Expansion Path
	// Expansion Self
	// Iterate Self
}

func (c *ModuleCallInstances) visit(exec *Executor) {
	// TODO Expand Expr
	// visitinstances
	for _, child := range *c {
		child.visit(exec)
	}
}

func (c *ModuleCallInstance) visit(exec *Executor) {
	c.ModuleInstance.visit(exec)
}

func (m *ModuleInstance) visit(exec *Executor) {
	modulePathKey := m.Addr.String()
	for _, call := range m.Module.Calls {
		instances, ok := call.InstancesByPath[modulePathKey]
		if !ok {
			instances = ModuleCallInstances{}
			call.InstancesByPath[modulePathKey] = instances
		}
		instances.visit(exec)
	}

	for _, resources := range m.Module.Resources {
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
