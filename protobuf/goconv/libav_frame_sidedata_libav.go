//go:build with_libav
// +build with_libav

package goconv

import (
	"github.com/asticode/go-astiav"
)

func FrameSideDataFromGo(input *astiav.FrameSideData) *FrameSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}

func (f *FrameSideData) Go() *astiav.FrameSideData {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
