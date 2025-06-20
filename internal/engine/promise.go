package engine

import (
	"errors"
	"fmt"
	"strings"
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
	reported bool
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

var mapping = map[workgraph.RequestID]string{}

func (p *Promise[T]) Value(caller executor) (T, tfdiags.Diagnostics) {
	p.lock.Lock()

	if len(caller.stack) != 0 {
		caller.edge(caller.stack[len(caller.stack)-1], p.ident.String())
	}
	caller.stack = append(caller.stack, p.ident.String())
	defer func() {
		caller.stack = caller.stack[:len(caller.stack)-1]
	}()

	if !p.running {
		p.resolver, p.promise = workgraph.NewRequest[T](caller.Worker)
		mapping[p.resolver.RequestID()] = p.ident.String()
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
	p.lock.Lock()
	if !p.reported {
		p.reported = true
		var selfErr workgraph.ErrSelfDependency
		if errors.As(err, &selfErr) {
			str := fmt.Sprintf("\nERROR!!!! Cycle Detected for %s: ", p.ident.String())
			for _, req := range selfErr.RequestIDs {
				str += " " + mapping[req]
			}
			str += "\n Stack: " + strings.Join(caller.stack, " => ")

			println(str)
		}
	}
	p.lock.Unlock()
	return t, tfdiags.Diagnostics{}.Append(err)
}
