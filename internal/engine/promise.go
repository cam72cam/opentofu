package engine

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/tfdiags"
)

type Ident struct {
	base   fmt.Stringer
	suffix string
}

func (i Ident) String() string {
	return fmt.Sprintf("%s %s", i.base, i.suffix)
}

type Promise[T any] struct {
	ident   fmt.Stringer
	resolve func(*Executor) (T, tfdiags.Diagnostics)

	cachedValue T
}

func NewPromise[T any](ident fmt.Stringer, resolve func(*Executor) (T, tfdiags.Diagnostics)) *Promise[T] {
	return &Promise[T]{
		ident:   ident,
		resolve: resolve,
	}
}

func (p *Promise[T]) Value(exec *Executor) (T, tfdiags.Diagnostics) {
	// Use exec to run resolver
	diags := exec.Execute(p, func(inner *Executor) tfdiags.Diagnostics {
		var diags tfdiags.Diagnostics
		p.cachedValue, diags = p.resolve(inner)
		return diags
	})
	return p.cachedValue, diags
}

func (p *Promise[T]) String() string {
	return p.ident.String()
}
