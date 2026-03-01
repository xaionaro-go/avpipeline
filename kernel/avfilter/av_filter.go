// Package avfilter provides libavfilter-based kernels.
package avfilter

import (
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame/condition"
)

// FrameFilter is the public interface for processing frames through filter graphs.
// Consumers interact with this interface to push frames in and pull frames out.
type FrameFilter interface {
	AddFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersrcFlags) error
	GetFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersinkFlags) error
	GetOutputStreams() []int
	Close() error
}

// GraphEndpoint is the internal interface used by Graph to wire up
// buffersrc/buffersink pairs. Consumers never need to interact with this.
type GraphEndpoint interface {
	FilterInput() *astiav.Filter
	FilterOutput() *astiav.Filter
	ConnectInput(graph *astiav.FilterGraph, nodeName string) error
	ConnectOutput(graph *astiav.FilterGraph, nodeName string) error
	InputFilterContext() *astiav.FilterContext
	OutputFilterContext() *astiav.FilterContext
}

// Kernel is a backward-compatible alias for FrameFilter.
// New code should use FrameFilter directly.
type Kernel = FrameFilter

// AVFilter wraps a FrameFilter with an optional Condition and Content string.
type AVFilter[T FrameFilter] struct {
	Condition condition.Condition
	Kernel    T
	Content   string
}

func (k *AVFilter[T]) String() string {
	return fmt.Sprintf("Filter(%v)", k.Kernel)
}

func (k *AVFilter[T]) AddFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersrcFlags) error {
	return k.Kernel.AddFrame(streamIdx, f, flags)
}

func (k *AVFilter[T]) GetFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersinkFlags) error {
	return k.Kernel.GetFrame(streamIdx, f, flags)
}

func (k *AVFilter[T]) GetOutputStreams() []int {
	return k.Kernel.GetOutputStreams()
}
