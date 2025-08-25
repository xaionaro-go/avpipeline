package indicator

import (
	"golang.org/x/exp/constraints"
)

type MovingAverage[T constraints.Integer | constraints.Float] interface {
	Update(v T) T
	InitPeriod() int64
	Valid() bool
}
