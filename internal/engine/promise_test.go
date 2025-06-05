package engine

import (
	"fmt"
	"sync"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestSimpleValid(t *testing.T) {
	var q *Promise[cty.Value]
	var p *Promise[cty.Value]

	p = NewPromise("var.foo", func() (cty.Value, error) {
		return cty.StringVal("Hello World"), nil
	})
	q = NewPromise("local.val", func() (cty.Value, error) {
		return p.Value(q)
	})

	result, err := q.Value(nil)
	t.Logf("%v, %v\n", result, err)
}
func TestSimpleCycle(t *testing.T) {
	var q *Promise[cty.Value]
	var p *Promise[cty.Value]

	p = NewPromise(5, func() (cty.Value, error) {
		return q.Value(p)
	})
	q = NewPromise("z", func() (cty.Value, error) {
		return p.Value(q)
	})

	result, err := q.Value(nil)
	t.Logf("%v, %v\n", result, err)
}

func TestSingleCycle(t *testing.T) {
	var n = 20
	chain := make([]*Promise[cty.Value], n, n)
	for i := 0; i < n; i++ {
		chain[i] = NewPromise(i, func() (cty.Value, error) {
			return chain[(i+1)%n].Value(chain[i])
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
		chain[i] = NewPromise(i, func() (cty.Value, error) {
			//time.Sleep(10 * time.Millisecond)
			val, err := chain[(i+1)%n].Value(chain[i])
			if err != nil {
				err = fmt.Errorf("%v unavailable due to %w", i, err)
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

func TestParallelCrazy(t *testing.T) {
	var n = 4000
	chain := make([]*Promise[cty.Value], n, n)
	for i := 0; i < n; i++ {
		chain[i] = NewPromise(i, func() (cty.Value, error) {
			//time.Sleep(10 * time.Millisecond)
			r := make(chan Result[cty.Value], 4)
			go func() {
				chain[(i+40)%n].Value(chain[i])
				chain[(i+80)%n].Value(chain[i])
				chain[(i+120)%n].Value(chain[i])
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
}
