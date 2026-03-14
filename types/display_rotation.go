// display_rotation.go defines the DisplayRotation type for atomically-updatable
// rotation angle, supporting mid-stream rotation changes (e.g., smartphone orientation).

package types

import (
	"math"
	"sync/atomic"
)

// DisplayRotation holds a rotation angle in degrees that can be atomically
// updated for mid-stream rotation changes (e.g., smartphone orientation changes).
// Use NaN to indicate that rotation should be read from codec parameters (default).
type DisplayRotation struct {
	bits atomic.Uint64
}

// NewDisplayRotation creates a new DisplayRotation with the given angle in degrees.
// Use math.NaN() to indicate "use rotation from codec parameters".
func NewDisplayRotation(degrees float64) *DisplayRotation {
	dr := &DisplayRotation{}
	dr.Store(degrees)
	return dr
}

// Store atomically sets the rotation angle in degrees.
func (dr *DisplayRotation) Store(degrees float64) {
	dr.bits.Store(math.Float64bits(degrees))
}

// Load atomically reads the rotation angle in degrees.
func (dr *DisplayRotation) Load() float64 {
	return math.Float64frombits(dr.bits.Load())
}

// IsSet returns true if the rotation has been explicitly set (not NaN).
func (dr *DisplayRotation) IsSet() bool {
	return !math.IsNaN(dr.Load())
}
