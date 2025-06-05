package engine

import (
	"fmt"
	"sync"

	"github.com/zclconf/go-cty/cty"
)

type Result struct {
	value cty.Value
	err   error
}

type Promise struct {
	target  any
	resolve func() (cty.Value, error)

	lock     sync.Mutex
	started  bool
	resolved *Result

	visitChan chan *Promise
	blockChan chan Blocked
}

type Blocked struct {
	resultChan chan Result
	promise    *Promise
}

func NewPromise(target any, resolve func() (cty.Value, error)) *Promise {
	p := &Promise{
		target:  target,
		resolve: resolve,
		// TODO tune chan size
		visitChan: make(chan *Promise, 100),
		blockChan: make(chan Blocked, 100),
	}

	return p
}

func (p *Promise) manager() {
	resultChan := make(chan Result, 1)
	go func() {
		value, err := p.resolve()
		resultChan <- Result{value, err}
	}()

	visits := map[any]*Promise{p.target: p}
	blocking := map[any]Blocked{}
	var waiters []Blocked

	debug := func(s string, args ...any) {
		//fmt.Printf("%v: %s\n", p.target, fmt.Sprintf(s, args...))
	}

	writeResolved := func(result Result) {
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
			debug(">blocking %v", blocked.promise.target)
			blocking[blocked.promise.target] = blocked

			// Cycle Check
			if _, ok := visits[blocked.promise.target]; ok {
				debug("cycle block")
				// If we have visited the thing that we are now blocking
				writeResolved(Result{
					err: fmt.Errorf("Cyclic dependency between %v and %v", p.target, blocked.promise.target),
				})
				return
			}

			// Back-propogate visits to new blocked
			for _, visit := range visits {
				blocked.promise.visitChan <- visit
			}
			debug("<blocking")
		case visit := <-p.visitChan:
			debug(">visit")
			visits[visit.target] = visit

			// Cycle Check
			if blocked, ok := blocking[visit.target]; ok {
				debug("cycle visit")
				// If we have visited something that we are blocking
				writeResolved(Result{
					err: fmt.Errorf("Cyclic dependency between %v and %v", p.target, blocked.promise.target),
				})
				return
			}

			// Back-propogate new visit to all blocked
			for _, blocked := range blocking {
				blocked.promise.visitChan <- visit
			}
			debug("<visit")
		}
	}

}

func (p *Promise) Value(caller *Promise) (cty.Value, error) {
	p.lock.Lock()
	if p.resolved != nil {
		fmt.Printf("Early %v\n", p.target)
		p.lock.Unlock()
		return p.resolved.value, p.resolved.err
	}

	if !p.started {
		go p.manager()
		p.started = true
	}

	resultChan := make(chan Result, 1)
	p.blockChan <- Blocked{
		resultChan: resultChan,
		promise:    caller,
	}
	p.lock.Unlock()

	result := <-resultChan
	return result.value, result.err
}
