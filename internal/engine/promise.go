package engine

import (
	"fmt"

	"github.com/apparentlymart/go-workgraph/workgraph"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

var mainWorker = workgraph.NewWorker()

type Promise[T any] struct {
	ident   fmt.Stringer
	promise workgraph.Promise[T]
}

type promise *workgraph.Worker

func NewPromise[T any](ident fmt.Stringer, resolve func(self promise) (T, tfdiags.Diagnostics)) *Promise[T] {
	resolver, promise := workgraph.NewRequest[T](mainWorker)

	//fmt.Printf("New Promise %s => %s\n", ident.String(), resolver.RequestID())

	workgraph.WithNewAsyncWorker(func(w *workgraph.Worker) {
		//fmt.Printf("Resolve Promise %s => %s\n", ident.String(), resolver.RequestID())

		t, diags := resolve(w)
		err := diags.Err()
		if err != nil {
			resolver.ReportError(w, err)
		} else {
			resolver.ReportSuccess(w, t)
		}

		//fmt.Printf("Resolved Promise %s => %s\n", ident.String(), resolver.RequestID())
	}, resolver)

	p := &Promise[T]{
		ident:   ident, // PTR for hashable
		promise: promise,
	}

	return p
}

func (p *Promise[T]) internalIdent() fmt.Stringer {
	return p.ident
}

func (p *Promise[T]) Value(caller promise) (T, tfdiags.Diagnostics) {
	if caller == nil {
		caller = workgraph.NewWorker()
	}
	t, err := p.promise.Await(caller)
	return t, tfdiags.Diagnostics{}.Append(err)
}
