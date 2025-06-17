package engine

import (
	"fmt"
)

type executor interface {
	Execute(any, func())
	Wait(executor, chan struct{}) error
	WaitingOn() executor
}

type Executor struct {
	stack     []any
	waitingOn executor
}

func NewExecutor() *Executor {
	return &Executor{}
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

func (e *Executor) Wait(waitingOn executor, wait chan struct{}) error {
	e.waitingOn = waitingOn
	defer func() {
		e.waitingOn = nil
	}()

	// Cycle check
	for w := waitingOn; w != nil; w = w.WaitingOn() {
		if w == e {
			return fmt.Errorf("TODO CYCLE")
		}
	}

	select {
	case <-wait:
	}
	return nil
}

func (e *Executor) Close() {

}
