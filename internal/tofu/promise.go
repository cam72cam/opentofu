package tofu

import (
	"fmt"
	"log"

	"github.com/opentofu/opentofu/internal/tfdiags"
)

type Identity interface {
	fmt.Stringer
	Addr() fmt.Stringer
}

type Ident struct {
	base   fmt.Stringer
	suffix string
}

func (i Ident) String() string {
	return fmt.Sprintf("%s %s", i.base, i.suffix)
}
func (i Ident) Addr() fmt.Stringer {
	return i.base
}

type Promise[T any] struct {
	ident   Identity
	resolve func(*Executor) (T, tfdiags.Diagnostics)

	cachedValue T
}

func NewPromise[T any](ident Identity, resolve func(*Executor) (T, tfdiags.Diagnostics)) *Promise[T] {
	return &Promise[T]{
		ident:   ident,
		resolve: resolve,
	}
}

func (p *Promise[T]) Value(exec *Executor) (T, tfdiags.Diagnostics) {
	// Use exec to run resolver
	diags := exec.Execute(p, func(inner *Executor) tfdiags.Diagnostics {
		log.Printf("[DEBUG] Resolving promise %s", p)
		var diags tfdiags.Diagnostics
		p.cachedValue, diags = p.resolve(inner)
		log.Printf("[DEBUG] Resolved promise %s", p)
		return diags
	})
	return p.cachedValue, diags
}

func (p *Promise[T]) String() string {
	return p.ident.String()
}

func (p *Promise[T]) Ident() Identity {
	return p.ident
}
