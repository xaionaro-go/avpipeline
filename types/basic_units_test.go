package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUB_Tob(t *testing.T) {
	assert.Equal(t, Ub(0), UB(0).Tob())
	assert.Equal(t, Ub(8), UB(1).Tob())
	assert.Equal(t, Ub(8000), UB(1000).Tob())
	assert.Equal(t, Ub(-8), UB(-1).Tob())
}

func TestUb_ToB(t *testing.T) {
	assert.Equal(t, UB(0), Ub(0).ToB())
	assert.Equal(t, UB(1), Ub(8).ToB())
	assert.Equal(t, UB(1000), Ub(8000).ToB())
	// Integer division truncates
	assert.Equal(t, UB(0), Ub(7).ToB())
}

func TestUB_ToBps(t *testing.T) {
	assert.InDelta(t, float64(UBps(10)), float64(UB(100).ToBps(US(10*time.Second))), 0.001)
	assert.InDelta(t, float64(UBps(1000)), float64(UB(1000).ToBps(US(time.Second))), 0.001)
	assert.InDelta(t, float64(UBps(2000)), float64(UB(1000).ToBps(US(500*time.Millisecond))), 0.001)
}

func TestUb_Tobps(t *testing.T) {
	assert.InDelta(t, float64(Ubps(80)), float64(Ub(800).Tobps(US(10*time.Second))), 0.001)
}

func TestUBps_ToB(t *testing.T) {
	assert.Equal(t, UB(50), UBps(10).ToB(US(5*time.Second)))
	assert.Equal(t, UB(0), UBps(10).ToB(US(0)))
	assert.Equal(t, UB(1000), UBps(1000).ToB(US(time.Second)))
}

func TestUbps_Tob(t *testing.T) {
	assert.Equal(t, Ub(400), Ubps(100).Tob(US(4*time.Second)))
}

func TestUBps_Tobps(t *testing.T) {
	assert.InDelta(t, float64(Ubps(8)), float64(UBps(1).Tobps()), 0.001)
	assert.InDelta(t, float64(Ubps(80000)), float64(UBps(10000).Tobps()), 0.001)
}

func TestUbps_ToBps(t *testing.T) {
	assert.InDelta(t, float64(UBps(1)), float64(Ubps(8).ToBps()), 0.001)
	assert.InDelta(t, float64(UBps(10000)), float64(Ubps(80000).ToBps()), 0.001)
}

func TestUB_ToS(t *testing.T) {
	got := UB(100).ToS(UBps(10))
	assert.InDelta(t, float64(10*time.Second), float64(got), float64(time.Millisecond))
}

func TestUb_ToS(t *testing.T) {
	got := Ub(800).ToS(Ubps(100))
	assert.InDelta(t, float64(8*time.Second), float64(got), float64(time.Millisecond))
}

func TestUS_ToB(t *testing.T) {
	assert.Equal(t, UB(40), US(4*time.Second).ToB(UBps(10)))
}

func TestUS_Tob(t *testing.T) {
	assert.Equal(t, Ub(400), US(4*time.Second).Tob(Ubps(100)))
}

func TestUS_String(t *testing.T) {
	assert.Equal(t, "5s", US(5*time.Second).String())
	assert.Equal(t, "1m0s", US(time.Minute).String())
	assert.Equal(t, "500ms", US(500*time.Millisecond).String())
}

func TestUB_String(t *testing.T) {
	s := UB(1000).String()
	assert.Contains(t, s, "B")
}

func TestUb_String(t *testing.T) {
	s := Ub(1000).String()
	assert.Contains(t, s, "b")
}

func TestUBps_String(t *testing.T) {
	s := UBps(1000).String()
	assert.Contains(t, s, "B/s")
}

func TestUbps_String(t *testing.T) {
	s := Ubps(1000).String()
	assert.Contains(t, s, "b/s")
}

func TestZeroConversions(t *testing.T) {
	assert.Equal(t, Ub(0), UB(0).Tob())
	assert.Equal(t, UB(0), Ub(0).ToB())
	assert.InDelta(t, float64(Ubps(0)), float64(UBps(0).Tobps()), 0.001)
	assert.InDelta(t, float64(UBps(0)), float64(Ubps(0).ToBps()), 0.001)
}

func TestRoundTripConversions(t *testing.T) {
	// Bytes → Bits → Bytes
	original := UB(42)
	assert.Equal(t, original, original.Tob().ToB())

	// BPS → bps → BPS
	originalBps := UBps(500)
	assert.InDelta(t, float64(originalBps), float64(originalBps.Tobps().ToBps()), 0.001)
}
