// onewaybool.go provides a one-way (sticky-true) atomic boolean latch.
//
// OneWayBool is a tiny wrapper around atomic.Bool whose contract is that
// the value can only ever transition from false to true, never back.
// This pins the sticky-true semantic at the type level so callers cannot
// accidentally clear the latch (the previous atomic.Bool exposed a
// Store(false) that contradicted the documented sticky-true contract;
// see avpipeline/preset/streammux/raw_frame_source_pixfmt.go).

package types

import "sync/atomic"

// OneWayBool is a sticky-true atomic boolean: once Set has been called,
// Load returns true forever. There is no API to clear the latch.
//
// Zero value is usable and reports Load() == false.
type OneWayBool struct {
	v atomic.Bool
}

// Set latches the value to true. It is idempotent: calling Set after the
// latch is already true is a no-op. Returns the previous value (true if
// the latch was already set).
func (b *OneWayBool) Set() (previous bool) {
	return b.v.Swap(true)
}

// Load returns the current latched value.
func (b *OneWayBool) Load() bool {
	return b.v.Load()
}
