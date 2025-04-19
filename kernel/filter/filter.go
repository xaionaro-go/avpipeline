package filter

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame/condition"
)

type Filter[T Kernel] struct {
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
}

func (k *Filter[T]) FilterInput() *astiav.Filter {
	return k.Kernel.FilterInput()
}

func (k *Filter[T]) FilterOutput() *astiav.Filter {
	return k.Kernel.FilterOutput()
}

func (k *Filter[T]) ConnectInput(graph *astiav.FilterGraph, nodeName string) {
	k.Kernel.ConnectInput(graph, nodeName)
}

func (k *Filter[T]) ConnectOutput(graph *astiav.FilterGraph, nodeName string) {
	k.Kernel.ConnectOutput(graph, nodeName)
}

func (k *Filter[T]) InputFilterContext() *astiav.FilterContext {
	return k.Kernel.InputFilterContext()
}

func (k *Filter[T]) OutputFilterContext() *astiav.FilterContext {
	return k.Kernel.OutputFilterContext()
}
