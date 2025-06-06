package engine

import (
	"context"
	"sync"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

// This is wired together a bit odd.  Instead of promising diagnostics as the "everything is done" value, it should instead return changes + state for each item
type WalkData struct {
	Context context.Context
	Cancel  context.CancelFunc
	Op      WalkOperation

	Config       *configs.Config
	InputState   *states.State
	InputChanges *plans.Changes
	InputVars    map[addrs.InputVariable]VariableInput
}

func Walk(data *WalkData) (*plans.Changes, *states.State, tfdiags.Diagnostics) {
	_, action, diags := NewModule(data.Context, addrs.RootModuleInstance, data.Config, data.InputChanges, data.InputState, nil, data.InputVars, data.Op)

	//checks := checks.NewState(data.Config)
	change := plans.NewChanges()
	state := states.NewState()

	// Flatten results to "standard" format
	recordDiags := action(change.SyncWrapper(), state.SyncWrapper())

	return change, state, diags.Append(recordDiags)
}

type Action func(*plans.ChangesSync, *states.SyncState) tfdiags.Diagnostics

type Actions []Action

func (actions Actions) Parallel(change *plans.ChangesSync, state *states.SyncState) tfdiags.Diagnostics {

	var pool = make(chan int, 10)
	for i := 0; i < 10; i++ {
		pool <- i
	}

	var diags tfdiags.Diagnostics

	var wg sync.WaitGroup
	var diagLock sync.Mutex

	for _, action := range actions {
		action := action
		if action == nil {
			continue
		}

		slot := <-pool

		wg.Add(1)
		go func() {

			actionDiags := action(change, state)
			diagLock.Lock()
			diags = diags.Append(actionDiags)
			diagLock.Unlock()
			wg.Done()
			pool <- slot
		}()
	}

	wg.Wait()

	return diags
}
