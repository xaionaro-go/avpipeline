// av_filter.go defines the AVFilter struct and Kernel interface for libavfilter.

// Package avfilter provides libavfilter-based kernels.
package avfilter

import (
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame/condition"
)

type AVFilter[T Kernel] struct {
	Condition condition.Condition
	Kernel    T
	Content   string
}

type Kernel interface {
	FilterInput() *astiav.Filter
	FilterOutput() *astiav.Filter
	ConnectInput(graph *astiav.FilterGraph, nodeName string) error
	ConnectOutput(graph *astiav.FilterGraph, nodeName string) error
	InputFilterContext() *astiav.FilterContext
	OutputFilterContext() *astiav.FilterContext
	AddFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersrcFlags) error
	GetFrame(streamIdx int, f *astiav.Frame, flags astiav.BuffersinkFlags) error
	GetOutputStreams() []int
	Close() error
}

func (k *AVFilter[T]) String() string {
	return fmt.Sprintf("Filter(%v)", k.Kernel)
}

func (k *AVFilter[T]) FilterInput() *astiav.Filter {
	return k.Kernel.FilterInput()
}

func (k *AVFilter[T]) FilterOutput() *astiav.Filter {
	return k.Kernel.FilterOutput()
}

func (k *AVFilter[T]) ConnectInput(graph *astiav.FilterGraph, nodeName string) error {
	return k.Kernel.ConnectInput(graph, nodeName)
}

func (k *AVFilter[T]) ConnectOutput(graph *astiav.FilterGraph, nodeName string) error {
	return k.Kernel.ConnectOutput(graph, nodeName)
}

func (k *AVFilter[T]) InputFilterContext() *astiav.FilterContext {
	return k.Kernel.InputFilterContext()
}

func (k *AVFilter[T]) OutputFilterContext() *astiav.FilterContext {
	return k.Kernel.OutputFilterContext()
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
