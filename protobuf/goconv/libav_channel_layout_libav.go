//go:build with_libav
// +build with_libav

package goconv

import (
	"github.com/asticode/go-astiav"
)

func ChannelLayoutFromGo(input *astiav.ChannelLayout) *ChannelLayout {
	if input == nil {
		return nil
	}
	return &ChannelLayout{
		//Order:      input.Order(),
		NbChannels: int32(input.Channels()),
		//U:          int64(input.U()),
	}
}

func (f *ChannelLayout) Go() *astiav.ChannelLayout {
	if f == nil {
		return nil
	}
	panic("not implemented")
}
