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

	//fmt.Printf("New Promise %s => %s\n", ident.String(), resolver.RequestID())

	/*workgraph.WithNewAsyncWorker(func(w *workgraph.Worker) {
		//fmt.Printf("Resolve Promise %s => %s\n", ident.String(), resolver.RequestID())

		t, diags := resolve(w)
		err := diags.Err()
		if err != nil {
			resolver.ReportError(w, err)
		} else {
			resolver.ReportSuccess(w, t)
		}

		//fmt.Printf("Resolved Promise %s => %s\n", ident.String(), resolver.RequestID())
	}, resolver)*/

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
	if caller == nil {
		caller = workgraph.NewWorker()
	}

	p.lock.Lock()
	if !p.running {
		p.resolver, p.promise = workgraph.NewRequest[T](caller)
		p.running = true
		p.lock.Unlock()

		t, diags := p.resolve(caller)
		err := diags.Err()
		if err != nil {
			p.resolver.ReportError(caller, err)
		} else {
			p.resolver.ReportSuccess(caller, t)
		}
	} else {
		p.lock.Unlock()
	}

	t, err := p.promise.Await(caller)
	return t, tfdiags.Diagnostics{}.Append(err)
}
