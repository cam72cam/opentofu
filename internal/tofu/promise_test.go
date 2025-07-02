package tofu

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type stringer string

func (s stringer) String() string {
	return string(s)
}

type istringer int

func (s istringer) String() string {
	return strconv.Itoa(int(s))
}

func TestSimpleValid(t *testing.T) {
	var q *Promise[cty.Value]
	var p *Promise[cty.Value]

	p = NewPromise[cty.Value](stringer("var.foo"), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
		return cty.StringVal("Hello World"), nil
	})
	q = NewPromise[cty.Value](stringer("local.val"), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
		return p.Value(e)
	})

	result, err := q.Value(nil)
	t.Logf("%v, %v\n", result, err)
}

func TestSimpleCycle(t *testing.T) {
	var q *Promise[cty.Value]
	var p *Promise[cty.Value]

	p = NewPromise[cty.Value](istringer(5), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
		return q.Value(e)
	})
	q = NewPromise[cty.Value](stringer("z"), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
		return p.Value(e)
	})

	result, err := q.Value(nil)
	t.Logf("%v, %v\n", result, err)
}

func TestSingleCycle(t *testing.T) {
	var n = 20
	chain := make([]*Promise[cty.Value], n, n)
	for i := 0; i < n; i++ {
		chain[i] = NewPromise[cty.Value](istringer(i), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
			return chain[(i+1)%n].Value(e)
		})
	}

	result, err := chain[0].Value(nil)
	t.Logf("%v, %v\n", result, err)
}

func TestParallelCycle(t *testing.T) {
	var n = 10
	chain := make([]*Promise[cty.Value], n, n)
	for i := 0; i < n; i++ {
		i := i
		chain[i] = NewPromise[cty.Value](istringer(i), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
			//time.Sleep(10 * time.Millisecond)
			val, err := chain[(i+1)%n].Value(e)
			if err != nil {
				err = err.Append(fmt.Errorf("%v unavailable due", i))
			}
			return val, err
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		result, err := chain[0].Value(nil)
		t.Logf("GO %v, %v\n", result, err)
	}()
	go func() {
		defer wg.Done()
		result, err := chain[n/2].Value(nil)
		t.Logf("GO %v, %v\n", result, err)
	}()
	wg.Wait()

	for _, c := range chain {
		result, err := c.Value(nil)
		t.Logf("%v, %v\n", result, err)
	}
}

/*
func TestParallelCrazy(t *testing.T) {
	var n = 4000
	chain := make([]*Promise[cty.Value], n, n)
	for i := 0; i < n; i++ {
		chain[i] = NewPromise[cty.Value](istringer(i), func(e *Executor) (cty.Value, tfdiags.Diagnostics) {
			//time.Sleep(10 * time.Millisecond)
			r := make(chan int, 4)
			go func() {
				chain[(i+40)%n].Value(e)
				chain[(i+80)%n].Value(e)
				chain[(i+120)%n].Value(e)
			}()
			for i := 0; i < 4; i++ {
				<-r
			}
			return cty.NilVal, nil
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		result, err := chain[0].Value(nil)
		t.Logf("%v, %v\n", result, err)
	}()
	go func() {
		defer wg.Done()
		result, err := chain[n/2].Value(nil)
		t.Logf("%v, %v\n", result, err)
	}()
	wg.Wait()
}*/
