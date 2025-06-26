package engine

import (
	"sync"

	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type Edge struct {
	Requester PoolEntry
	Requestee PoolEntry
}

type Manager struct {
	wg sync.WaitGroup

	backing *Pool

	sem tofu.Semaphore
}

func NewManager(sem tofu.Semaphore) *Manager {
	return &Manager{
		backing: &Pool{
			data: map[PoolEntry]*PoolData{},
			sem:  sem,
		},
		sem: sem,
	}
}

func (c *Manager) Add(p ValuePromise) {
	c.wg.Add(1)
	c.sem.Acquire()
	go func() {
		defer c.wg.Done()
		defer c.sem.Release()
		p.Value(NewExecutor(nil, c.backing))
	}()
}
func (c *Manager) Expand(e ExpandValuePromise) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		e.Expand(c, NewExecutor(nil, c.backing))
	}()
}

func (c *Manager) Wait() ([]Edge, tfdiags.Diagnostics) {
	c.wg.Wait()

	var edges []Edge
	for key, entry := range c.backing.data {
		for _, visit := range entry.visited {
			edges = append(edges, Edge{key, visit})
		}
	}

	return edges, c.backing.diags
}

type ValuePromise interface {
	Value(*Executor) (cty.Value, tfdiags.Diagnostics)
}

type ExpandValuePromise interface {
	ValuePromise
	Expand(*Manager, *Executor) // Could also return map[addrs.InstanceKey]*Scope for DependsOn
}
