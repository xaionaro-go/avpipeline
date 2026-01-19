// filter.go implements a filter that adds pipeline side data to packets or frames.

// Package addpipelinesidedata provides a filter that pipeline adds side data to packets or frames.
package addpipelinesidedata

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// Filter implements a filter that adds side data to packets or frames.
type Filter struct {
	SideData any
}

// New creates a new filter that adds the specified side data to packets or frames.
func New(sideData any) *Filter {
	return &Filter{SideData: sideData}
}

// Match adds the side data to the input union and returns true.
func (f *Filter) Match(ctx context.Context, i packetorframe.InputUnion) bool {
	i.AddPipelineSideData(f.SideData)
	return true
}

// String returns a string representation of the filter.
func (f *Filter) String() string {
	return fmt.Sprintf("AddSideData(%T)", f.SideData)
}
