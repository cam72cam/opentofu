// SPDX-License-Identifier: MPL-2.0

package addrs

import (
	"fmt"
)

// ConstValue is the address of a const value.
type ConstValue struct {
	referenceable
	Name string
}

func (v ConstValue) String() string {
	return "const." + v.Name
}

func (v ConstValue) UniqueKey() UniqueKey {
	return v // A ConstValue is its own UniqueKey
}

func (v ConstValue) uniqueKeySigil() {}

// Absolute converts the receiver into an absolute address within the given
// module instance.
func (v ConstValue) Absolute(m ModuleInstance) AbsConstValue {
	return AbsConstValue{
		Module:     m,
		ConstValue: v,
	}
}

// AbsConstValue is the absolute address of a const value within a module instance.
type AbsConstValue struct {
	Module     ModuleInstance
	ConstValue ConstValue
}

// ConstValue returns the absolute address of a const value of the given
// name within the receiving module instance.
func (m ModuleInstance) ConstValue(name string) AbsConstValue {
	return AbsConstValue{
		Module: m,
		ConstValue: ConstValue{
			Name: name,
		},
	}
}

func (v AbsConstValue) String() string {
	if len(v.Module) == 0 {
		return v.ConstValue.String()
	}
	return fmt.Sprintf("%s.%s", v.Module.String(), v.ConstValue.String())
}
