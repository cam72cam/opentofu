package engine

import (
	"fmt"

	"github.com/apparentlymart/go-workgraph/workgraph"
)

type executor struct {
	*workgraph.Worker
	edge    func(any, any)
	current any
}

func NewExecutor(edge func(any, any)) executor {
	return executor{Worker: workgraph.NewWorker(), edge: edge}
}

type Ident struct {
	base   fmt.Stringer
	suffix string
}

func (i Ident) String() string {
	return fmt.Sprintf("%s %s", i.base, i.suffix)
}

/*
	Visit(any)
	Execute(any, func())
	Wait(any, executor, chan struct{}) error
	WaitingOn() executor
}

type Executor struct {
	recorder  func(any, any)
	stack     []any
	waitingOn executor
}

func NewExecutor(recorder func(any, any)) *Executor {
	return &Executor{recorder: recorder}
}

func (e *Executor) Visit(id any) {
	if len(e.stack) != 0 {
		e.recorder(e.stack[len(e.stack)-1], id)
	}
}

func (e *Executor) Execute(id any, action func()) {
	// Push on to stack
	e.stack = append(e.stack, id)

	action()

	// Pop off of stack
	e.stack = e.stack[:len(e.stack)-1]
}

func (e *Executor) WaitingOn() executor {
	return e.waitingOn
}

func (e *Executor) Wait(id any, waitingOn executor, wait chan struct{}) error {
	e.waitingOn = waitingOn
	defer func() {
		e.waitingOn = nil
	}()

	// Cycle check
	hasCycle := false
	for w := waitingOn; w != nil; w = w.WaitingOn() {
		if w == e {
			hasCycle = true
			break
		}
	}
	if hasCycle {
		// Create stack trace
		var stack []any
		for w := waitingOn; w != nil; w = w.WaitingOn() {
			stack = append(stack, w.(*Executor).stack...)
			if w == e {
				break
			}
		}
		stack = append(stack, id)

		var msg string
		foundCycleStart := false
		for _, item := range stack {
			if !foundCycleStart {
				if item == id {
					foundCycleStart = true
					msg = fmt.Sprintf("Cycle Detected: %s", item)
				}
				continue
			}
			if foundCycleStart {
				msg = fmt.Sprintf("%s -> %s", msg, item)
			}

		}
		return errors.New(msg)
	}

	select {
	case <-wait:
	}
	return nil
}

func (e *Executor) Close() {

}
*/
