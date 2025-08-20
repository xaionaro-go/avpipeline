package types

import (
	"slices"
)

type PipelineSideData []any

func (p PipelineSideData) Contains(data any) bool {
	return slices.Contains(p, data)
}
