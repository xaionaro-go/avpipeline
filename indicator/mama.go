// mama.go implements the MESA Adaptive Moving Average (MAMA) indicator.

package indicator

import (
	"sync"

	indicators "github.com/lmpizarro/go_ehlers_indicators"
	"golang.org/x/exp/constraints"
)

type MAMA[T constraints.Integer | constraints.Float] struct {
	FastLimit           float64
	SlowLimit           float64
	values              []float64
	orderedValuesBuffer []float64
	curIdx              int
	measurementsCount   int
	locker              sync.Mutex
}

var _ MovingAverage[int64] = (*MAMA[int64])(nil)

func NewMAMADefault[T constraints.Integer | constraints.Float](
	n int,
) *MAMA[T] {
	return NewMAMA[T](n, 0.5, 0.05)
}

func NewMAMA[T constraints.Integer | constraints.Float](
	n int,
	fastLimit float64,
	slowLimit float64,
) *MAMA[T] {
	return &MAMA[T]{
		FastLimit:           fastLimit,
		SlowLimit:           slowLimit,
		values:              make([]float64, n),
		orderedValuesBuffer: make([]float64, n),
	}
}

func (m *MAMA[T]) Update(v T) T {
	m.locker.Lock()
	defer m.locker.Unlock()

	m.values[m.curIdx] = float64(v)
	m.curIdx = (m.curIdx + 1) % len(m.values)

	// raw      3 4 5 6 7 0 1 2
	//                  ^ curIdx
	// ordered  0 1 2 3 4 5 6 7
	copy(m.orderedValuesBuffer, m.values[m.curIdx:])
	copy(m.orderedValuesBuffer[len(m.values)-m.curIdx:], m.values)

	m.measurementsCount++
	if m.measurementsCount < len(m.values) {
		return v
	}

	result := indicators.MAMA(m.orderedValuesBuffer, m.FastLimit, m.SlowLimit)
	return T(result[len(result)-1])
}

func (m *MAMA[T]) InitPeriod() int64 {
	return int64(len(m.values))
}

func (m *MAMA[T]) Valid() bool {
	return m.measurementsCount >= len(m.values)
}
