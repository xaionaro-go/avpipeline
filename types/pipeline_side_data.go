// pipeline_side_data.go defines the PipelineSideData type for storing side data in the pipeline.

package types

import (
	"slices"
)

type PipelineSideData []any

func (p PipelineSideData) Contains(data any) bool {
	return slices.Contains(p, data)
}
