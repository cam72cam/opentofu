package gohcl

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

type Attr[T any] struct {
	Range     hcl.Range
	NameRange hcl.Range
	Value     T
}

func (a Attr[T]) String() string {
	return fmt.Sprintf("%#v", a)
}
