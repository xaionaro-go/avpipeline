//go:build with_libav
// +build with_libav

package goconv

import (
	"github.com/asticode/go-astiav"
)

func RationalFromGo(input *astiav.Rational) *Rational {
	if input == nil {
		return nil
	}
	return &Rational{
		N: int64(input.Num()),
		D: int64(input.Den()),
	}
}

func (f *Rational) Go() *astiav.Rational {
	if f == nil {
		return nil
	}
	return ptr(astiav.NewRational(int(f.N), int(f.D)))
}
