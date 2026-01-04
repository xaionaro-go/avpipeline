// Package symbolresolver provides RPN symbol resolution for the AV pipeline.
//
// variable.go defines a variable-based symbol resolver.
package symbolresolver

import (
	rpntypes "github.com/xaionaro-go/rpn/types"
)

type VariableT[A any, R rpntypes.Number] struct {
	Name    string
	Pointer *R
}

var _ rpntypes.SymbolResolver[any, float64] = (*VariableT[any, float64])(nil)

func Variable[A any, R rpntypes.Number](name string, pointer *R) *VariableT[A, R] {
	return &VariableT[A, R]{
		Name:    name,
		Pointer: pointer,
	}
}

func (v *VariableT[A, R]) Resolve(name string) (rpntypes.ValueLoader[A, R], error) {
	if name != v.Name {
		return nil, nil
	}
	return rpntypes.FuncValue[A, R](func(arg A) R {
		return *v.Pointer
	}), nil
}
