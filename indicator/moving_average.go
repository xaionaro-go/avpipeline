// moving_average.go defines the MovingAverage interface for technical indicators.

// Package indicator provides various technical indicators for data analysis.
package indicator

import (
	"golang.org/x/exp/constraints"
)

type MovingAverage[T constraints.Integer | constraints.Float] interface {
	Update(v T) T
	InitPeriod() int64
	Valid() bool
}
