// pipeline_side_data.go defines the PipelineSideData type for storing side data in the pipeline.

package types

import (
	"slices"
)

type PipelineSideData []any

func (p PipelineSideData) Contains(data any) bool {
	return slices.Contains(p, data)
}

func PipelineSideDataLatest[T any](p PipelineSideData) (ret T, ok bool) {
	for i := len(p) - 1; i >= 0; i-- {
		if v, ok := p[i].(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}
