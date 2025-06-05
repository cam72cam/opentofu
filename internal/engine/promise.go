package engine

import (
	"fmt"
	"sync"

	"github.com/opentofu/opentofu/internal/tfdiags"
)

type Promise[T any] struct {
	target  any
	resolve func() (T, tfdiags.Diagnostics)

	lock     sync.Mutex
	started  bool
	resolved *Result[T]

	visitChan chan promise
	blockChan chan Blocked[T]
}

type promise interface {
	internalTarget() any
	addVisit(promise)
}

type Result[T any] struct {
	value T
	diags tfdiags.Diagnostics
}

type Blocked[T any] struct {
	resultChan chan Result[T]
	promise    promise
}

func NewPromise[T any](target any, resolve func() (T, tfdiags.Diagnostics)) *Promise[T] {
	p := &Promise[T]{
		target:  target,
		resolve: resolve,
		// TODO tune chan size
		visitChan: make(chan promise, 100),
		blockChan: make(chan Blocked[T], 100),
	}

	return p
}

func (p *Promise[T]) internalTarget() any {
	return p.target
}
func (p *Promise[T]) addVisit(visit promise) {
	p.visitChan <- visit
}

func (p *Promise[T]) manager() {
	resultChan := make(chan Result[T], 1)
	go func() {
		value, err := p.resolve()
		resultChan <- Result[T]{value, err}
	}()

	visits := map[any]promise{p.target: p}
	blocking := map[any]Blocked[T]{}
	var waiters []Blocked[T]

	debug := func(s string, args ...any) {
		//fmt.Printf("%v: %s\n", p.target, fmt.Sprintf(s, args...))
	}

	writeResolved := func(result Result[T]) {
		debug("resolved")
		p.lock.Lock()
		p.resolved = &result
		p.lock.Unlock()

		// Flush remaining waiters
		debug("flush")
		close(p.blockChan)
		for blocked := range p.blockChan {
			blocked.resultChan <- result
		}
		for _, blocked := range blocking {
			blocked.resultChan <- result
		}
		for _, blocked := range waiters {
			blocked.resultChan <- result
		}
		debug("done")

		return
	}

	debug("loop manager")
	for {
		select {
		case result := <-resultChan:
			debug("result")
			writeResolved(result)
			return
		case blocked := <-p.blockChan:
			if blocked.promise == nil {
				debug("waiter")
				waiters = append(waiters, blocked)
				continue
			}
			debug(">blocking %v", blocked.promise.internalTarget())
			blocking[blocked.promise.internalTarget()] = blocked

			// Cycle Check
			if _, ok := visits[blocked.promise.internalTarget()]; ok {
				debug("cycle block")
				// If we have visited the thing that we are now blocking
				writeResolved(Result[T]{
					diags: tfdiags.Diagnostics{}.Append(fmt.Errorf("Cyclic dependency between %v and %v", p.target, blocked.promise.internalTarget())),
				})
				return
			}

			// Back-propogate visits to new blocked
			for _, visit := range visits {
				blocked.promise.addVisit(visit)
			}
			debug("<blocking")
		case visit := <-p.visitChan:
			debug(">visit")
			visits[visit.internalTarget()] = visit

			// Cycle Check
			if blocked, ok := blocking[visit.internalTarget()]; ok {
				debug("cycle visit")
				// If we have visited something that we are blocking
				writeResolved(Result[T]{
					diags: tfdiags.Diagnostics{}.Append(fmt.Errorf("Cyclic dependency between %v and %v", p.target, blocked.promise.internalTarget())),
				})
				return
			}

			// Back-propogate new visit to all blocked
			for _, blocked := range blocking {
				blocked.promise.addVisit(visit)
			}
			debug("<visit")
		}
	}

}

func (p *Promise[T]) Value(caller promise) (T, tfdiags.Diagnostics) {
	p.lock.Lock()
	if p.resolved != nil {
		fmt.Printf("Early %v\n", p.target)
		p.lock.Unlock()
		return p.resolved.value, p.resolved.diags
	}

	if !p.started {
		go p.manager()
		p.started = true
	}

	resultChan := make(chan Result[T], 1)
	p.blockChan <- Blocked[T]{
		resultChan: resultChan,
		promise:    caller,
	}
	p.lock.Unlock()

	result := <-resultChan
	return result.value, result.diags
}
