package engine

import (
	"fmt"
	"sync"

	"github.com/opentofu/opentofu/internal/tfdiags"
)

type PromiseStatus int

const (
	// Has not yet started
	PromiseStatusUnresolved PromiseStatus = iota
	// An executor is resolving this
	PromiseStatusResolving
	// Discovered another executor is already resolving this node
	PromiseStatusResolved
)

type Ident struct {
	base   fmt.Stringer
	suffix string
}

func (i Ident) String() string {
	return fmt.Sprintf("%s %s", i.base, i.suffix)
}

type Promise[T any] struct {
	ident   fmt.Stringer
	resolve func(executor) (T, tfdiags.Diagnostics)

	lock        sync.Mutex
	status      PromiseStatus
	cachedValue T
	cachedDiags tfdiags.Diagnostics
	resolved    chan struct{}
	owner       executor
}

func NewPromise[T any](ident fmt.Stringer, resolve func(executor) (T, tfdiags.Diagnostics)) *Promise[T] {
	return &Promise[T]{
		ident:   ident,
		resolve: resolve,

		status:   PromiseStatusUnresolved,
		resolved: make(chan struct{}),
	}
}

func (p *Promise[T]) Value(exec executor) (T, tfdiags.Diagnostics) {
	if exec == nil {
		e := NewExecutor()
		defer e.Close()
		exec = e
	}

	// Quick attempt to return early
	if p.status == PromiseStatusResolved {
		return p.cachedValue, p.cachedDiags
	}

	p.lock.Lock()
	switch p.status {
	case PromiseStatusUnresolved:
		// Move to resolving
		p.status = PromiseStatusResolving
		p.owner = exec
		p.lock.Unlock()

		// Use exec to run resolver
		exec.Execute(p, func() {
			p.cachedValue, p.cachedDiags = p.resolve(exec)
		})

		p.lock.Lock()
		// Move to resolved
		p.status = PromiseStatusResolved
		// Notify waiters
		close(p.resolved)
		// Remove owner
		p.owner = nil
		p.lock.Unlock()
	case PromiseStatusResolving:
		// Another executor is already handling this node, check for cycle and release executor for now
		fmt.Printf("Need Wait %v\n", exec)
		p.lock.Unlock()
		// Let the executor know to wait for the resolution or cycle
		if err := exec.Wait(p, p.owner, p.resolved); err != nil {
			return p.cachedValue, p.cachedDiags.Append(err)
		}
	case PromiseStatusResolved:
		p.lock.Unlock()
	}

	return p.cachedValue, p.cachedDiags
}

func (p *Promise[T]) String() string {
	return p.ident.String()
}
