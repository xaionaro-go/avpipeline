// onewaybool_test.go pins the sticky-true contract of OneWayBool.

package types

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOneWayBool_ZeroValueFalse pins that the zero value reports Load()==false
// without any explicit construction. This matters because the type is embedded
// in larger structs (StreamMux, Output) and Go zero-init must produce the
// "not yet latched" state.
func TestOneWayBool_ZeroValueFalse(t *testing.T) {
	var b OneWayBool
	assert.False(t, b.Load(), "zero value must be false")
}

// TestOneWayBool_SetLatchesTrue pins the GOOD-side: Set() flips to true and
// Load() observes it.
func TestOneWayBool_SetLatchesTrue(t *testing.T) {
	var b OneWayBool
	prev := b.Set()
	assert.False(t, prev, "first Set must report previous=false")
	assert.True(t, b.Load(), "Set must latch to true")
}

// TestOneWayBool_SetIdempotent pins that repeated Set() calls do not break
// the latch and report previous=true after the first call.
func TestOneWayBool_SetIdempotent(t *testing.T) {
	var b OneWayBool
	require.False(t, b.Set(), "sanity")
	prev := b.Set()
	assert.True(t, prev, "second Set must report previous=true")
	assert.True(t, b.Load(), "still true after repeated Set")
}

// TestOneWayBool_NoClearAPI is the design proof: there must be no public
// method that returns the value to false. The compiler enforces this; this
// test documents it.
func TestOneWayBool_NoClearAPI(t *testing.T) {
	var b OneWayBool
	b.Set()
	// The only public methods are Set and Load. Set never clears (verified
	// above via TestOneWayBool_SetIdempotent). Load is read-only.
	assert.True(t, b.Load(), "no API path can clear the latch")
}

// TestOneWayBool_ConcurrentSet pins thread safety: concurrent Set() calls
// must all observe true after the dust settles, with exactly one reporting
// previous=false.
func TestOneWayBool_ConcurrentSet(t *testing.T) {
	const goroutines = 64
	var b OneWayBool
	var wg sync.WaitGroup
	prevFalseCount := make(chan struct{}, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if !b.Set() {
				prevFalseCount <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(prevFalseCount)
	count := 0
	for range prevFalseCount {
		count++
	}
	assert.Equal(t, 1, count,
		"exactly one concurrent Set must observe previous=false")
	assert.True(t, b.Load(), "post-race latch must be true")
}
