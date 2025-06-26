package engine

import (
	"sync"

	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type Edge struct {
	Requester any
	Requestee any
}

type ConcurrencyPool struct {
	wg   sync.WaitGroup
	pool chan struct{}

	backing *Pool
}

func NewConcurrencyPool(size int) *ConcurrencyPool {
	c := make(chan struct{}, size)
	for i := 0; i < size; i++ {
		c <- struct{}{}
	}
	return &ConcurrencyPool{
		pool: c,
		backing: &Pool{
			data: map[PoolEntry]*PoolData{},
		},
	}
}

func (c *ConcurrencyPool) Add(p ValuePromise) {
	c.wg.Add(1)
	// TODO run these in a limited set of routines
	go func() {
		defer c.wg.Done()
		slot := <-c.pool
		p.Value(NewExecutor(nil, c.backing))
		c.pool <- slot
	}()
}
func (c *ConcurrencyPool) Expand(e ExpandValuePromise) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		slot := <-c.pool
		e.Expand(c, NewExecutor(nil, c.backing))
		c.pool <- slot
	}()
}

func (c *ConcurrencyPool) Wait() ([]Edge, tfdiags.Diagnostics) {
	c.wg.Wait()
	// TODO collect edges
	return nil, c.backing.diags
}

type ValuePromise interface {
	Value(*Executor) (cty.Value, tfdiags.Diagnostics)
}

type ExpandValuePromise interface {
	ValuePromise
	Expand(*ConcurrencyPool, *Executor) // Could also return map[addrs.InstanceKey]*Scope for DependsOn
}
