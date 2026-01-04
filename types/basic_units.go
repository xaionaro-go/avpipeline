// units.go defines custom types for time, data size, and bitrate units.

package types

import (
	"time"

	"github.com/dustin/go-humanize"
)

type (
	US   time.Duration // s
	UB   int64         // B
	Ub   int64         // b
	UBps float64       // B/s
	Ubps float64       // b/s
)

func (v UB) Tob() Ub {
	return Ub(v * 8)
}

func (v Ub) ToB() UB {
	return UB(v / 8)
}

func (v UB) ToBps(t US) UBps {
	return UBps(float64(v) / (float64(t) / float64(time.Second)))
}

func (v Ub) Tobps(t US) Ubps {
	return Ubps(float64(v) / (float64(t) / float64(time.Second)))
}

func (v UBps) ToB(t US) UB {
	return UB(float64(v) * float64(t) / float64(time.Second))
}

func (v Ubps) Tob(t US) Ub {
	return Ub(float64(v) * float64(t) / float64(time.Second))
}

func (v UBps) Tobps() Ubps {
	return Ubps(v * 8)
}

func (v Ubps) ToBps() UBps {
	return UBps(v / 8)
}

func (v UB) ToS(r UBps) US {
	return US(float64(v) / float64(r) * float64(time.Second))
}

func (v Ub) ToS(r Ubps) US {
	return US(float64(v) / float64(r) * float64(time.Second))
}

func (v US) ToB(r UBps) UB {
	return UB(float64(v) * float64(r) / float64(time.Second))
}

func (v US) Tob(r Ubps) Ub {
	return Ub(float64(v) * float64(r) / float64(time.Second))
}

func (v US) String() string {
	return time.Duration(v).String()
}

func (v UB) String() string {
	return humanize.SI(float64(v), "B")
}

func (v Ub) String() string {
	return humanize.SI(float64(v), "b")
}

func (v UBps) String() string {
	return humanize.SI(float64(v), "B/s")
}

func (v Ubps) String() string {
	return humanize.SI(float64(v), "b/s")
}
