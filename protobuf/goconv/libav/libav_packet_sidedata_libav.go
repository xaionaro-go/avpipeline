//go:build with_libav
// +build with_libav

package libav

import (
	"github.com/asticode/go-astiav"
)

func PacketSideDataFromGo(input *astiav.PacketSideData) *PacketSideData {
	if input == nil {
		return nil
	}
	// not implemented
	return nil
}

func (f *PacketSideData) Go() *astiav.PacketSideData {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
