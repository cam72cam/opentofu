package engine

import (
	"fmt"
	"sync"

	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
)

type PoolEntryStatus int

const (
	PoolEntryStatusUnresolved PoolEntryStatus = iota
	// An executor is resolving this
	PoolEntryStatusResolving
	// Discovered another executor is already resolving this node
	PoolEntryStatusResolved
)

type PoolEntry interface {
	fmt.Stringer
	Ident() Identity
}
type PoolData struct {
	sync.Mutex

	status PoolEntryStatus
	waiter chan struct{}

	visiting PoolEntry
	visited  []PoolEntry
	diags    tfdiags.Diagnostics

	ancestors          []PoolEntry
	ancestorsPopulated bool
}

type Pool struct {
	sync.Mutex

	data map[PoolEntry]*PoolData

	diags tfdiags.Diagnostics

	sem tofu.Semaphore
}

func (p *Pool) Ancestors(root PoolEntry) []PoolEntry {
	// DFS walk
	p.Lock()
	defer p.Unlock()

	var down func(PoolEntry) []PoolEntry
	down = func(entry PoolEntry) []PoolEntry {
		data := p.data[entry]
		if !data.ancestorsPopulated {
			data.ancestorsPopulated = true

			// Use a map for dedup
			entries := make(map[PoolEntry]struct{})
			for _, visit := range data.visited {
				entries[visit] = struct{}{}
				for _, found := range down(visit) {
					entries[found] = struct{}{}
				}
			}

			for visit := range entries {
				data.ancestors = append(data.ancestors, visit)
			}

			return data.ancestors
		}

		return data.ancestors
	}
	return down(root)
}

type Executor struct {
	caller PoolEntry
	pool   *Pool
}

func (e *Executor) Execute(p PoolEntry, resolve func(*Executor) tfdiags.Diagnostics) tfdiags.Diagnostics {
	// Lock for modifications to the pool
	e.pool.Lock()
	entry, ok := e.pool.data[p]
	if !ok {
		entry = &PoolData{
			status: PoolEntryStatusUnresolved,
			waiter: make(chan struct{}),
		}
		e.pool.data[p] = entry
	}

	if e.caller != nil {
		// Record visit regardless of status
		caller := e.pool.data[e.caller]
		caller.Lock()
		caller.visiting = p
		caller.visited = append(caller.visited, p) // Not deduped (for now?)
		caller.Unlock()
	}

	// Done with direct access to the pool map
	e.pool.Unlock()

	entry.Lock()
	switch entry.status {
	case PoolEntryStatusUnresolved:
		// Start resolver
		entry.status = PoolEntryStatusResolving
		entry.Unlock()

		// We will be the only ones to modify the entry from here on (other than visited)
		// Keep the call stack short by running each in it's own go routine
		go func() {
			defer func() {
				close(entry.waiter)
			}()
			entry.diags = resolve(NewExecutor(p, e.pool))
			if entry.diags.HasErrors() {
				// Visited won't be modified past here
				e.pool.Lock()
				defer e.pool.Unlock()
				for _, key := range entry.visited {
					if e.pool.data[key].diags.HasErrors() {
						// something we called failed, we don't need to report our diags
						return
					}
				}
				e.pool.diags = e.pool.diags.Append(entry.diags)
			}
		}()
	case PoolEntryStatusResolving:
		// Check for cycle
		entry.Unlock()

		// We are not executing, free up a slot until the wait is over
		//e.pool.sem.Release()
		//defer e.pool.sem.Acquire()

		// Conservative lock
		e.pool.Lock()
		// TODO optimize this a bit...
		// Could use some pointer magic?
		stack := []PoolEntry{}
		for current := e.pool.data[entry.visiting]; current != nil; current = e.pool.data[current.visiting] {
			stack = append(stack, current.visiting)
			if current == entry {
				stack = append(stack, stack[0])
				err := fmt.Errorf("CYCLE: %v", stack)
				//e.pool.diags = e.pool.diags.Append(err)
				e.pool.Unlock()
				return tfdiags.Diagnostics{}.Append(err)
			}

		}
		e.pool.Unlock()
	}

	select {
	case <-entry.waiter:
		// Wait for completion (if applicable)
	}

	return entry.diags
}

func NewExecutor(caller PoolEntry, pool *Pool) *Executor {
	return &Executor{
		caller: caller,
		pool:   pool,
	}
}
