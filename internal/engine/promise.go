package engine

import (
	"fmt"
	"sync"

	"github.com/apparentlymart/go-workgraph/workgraph"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

var mainWorker = workgraph.NewWorker()

type Promise[T any] struct {
	ident    fmt.Stringer
	promise  workgraph.Promise[T]
	resolver workgraph.Resolver[T]
	lock     sync.Mutex
	running  bool
	resolve  func(self executor) (T, tfdiags.Diagnostics)
}

func NewPromise[T any](ident fmt.Stringer, resolve func(self executor) (T, tfdiags.Diagnostics)) *Promise[T] {
	p := &Promise[T]{
		ident:   ident, // PTR for hashable
		resolve: resolve,
	}

	return p
}

func (p *Promise[T]) internalIdent() fmt.Stringer {
	return p.ident
}

func (p *Promise[T]) Value(caller executor) (T, tfdiags.Diagnostics) {
	p.lock.Lock()

	prev := caller.current
	if prev != nil {
		caller.edge(prev, p)
	}
	caller.current = p
	defer func() {
		caller.current = prev
	}()

	if !p.running {
		p.resolver, p.promise = workgraph.NewRequest[T](caller.Worker)
		p.running = true
		p.lock.Unlock()

		t, diags := p.resolve(caller)
		err := diags.Err()
		if err != nil {
			p.resolver.ReportError(caller.Worker, err)
		} else {
			p.resolver.ReportSuccess(caller.Worker, t)
		}
	} else {
		p.lock.Unlock()
	}

	t, err := p.promise.Await(caller.Worker)
	return t, tfdiags.Diagnostics{}.Append(err)
}
