//go:build with_libav
// +build with_libav

package goconv

import (
	"github.com/asticode/go-astiav"
)

func FormatContextFromGo(input *astiav.FormatContext) *FormatContext {
	if input == nil {
		return nil
	}
	result := &FormatContext{}
	for _, stream := range input.Streams() {
		result.Streams = append(result.Streams, StreamFromGo(stream).Protobuf())
	}
	return result
}

func (fmtCtx *FormatContext) Go() *astiav.FormatContext {
	if fmtCtx == nil {
		return nil
	}
	panic("not implemented")
}
