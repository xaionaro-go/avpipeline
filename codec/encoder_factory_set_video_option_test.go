// encoder_factory_set_video_option_test.go pins the contract of the
// SetVideoOption / GetVideoOption / SetVideoOptionIfAbsent helpers added
// to encapsulate the encoder factory's VideoOptions Dictionary mutation.
// Cross-package callers (preset/streammux's late pix_fmt injection)
// used to reach directly into f.VideoOptions; the helpers move the
// boundary inside the factory.

package codec

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNaiveEncoderFactory_SetVideoOption_AllocatesDictionary pins that
// SetVideoOption allocates the VideoOptions Dictionary lazily when nil.
// Without this, cross-package callers would have had to allocate the
// Dictionary themselves before writing — exactly the boundary leak this
// helper closes.
func TestNaiveEncoderFactory_SetVideoOption_AllocatesDictionary(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	require.Nil(t, f.VideoOptions, "sanity: zero-value Dictionary is nil")

	require.NoError(t, f.SetVideoOption(ctx, "pix_fmt", "nv12"))

	require.NotNil(t, f.VideoOptions, "SetVideoOption must allocate Dictionary on first call")
	got := f.GetVideoOption(ctx, "pix_fmt")
	require.NotNil(t, got)
	assert.Equal(t, "nv12", *got)
}

// TestNaiveEncoderFactory_SetVideoOption_OverwritesExisting pins that
// SetVideoOption clobbers a prior value (this is the ordinary Set
// semantic; SetVideoOptionIfAbsent is the no-clobber variant).
func TestNaiveEncoderFactory_SetVideoOption_OverwritesExisting(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	require.NoError(t, f.SetVideoOption(ctx, "pix_fmt", "yuv420p"))
	require.NoError(t, f.SetVideoOption(ctx, "pix_fmt", "nv12"))
	got := f.GetVideoOption(ctx, "pix_fmt")
	require.NotNil(t, got)
	assert.Equal(t, "nv12", *got, "second SetVideoOption must overwrite the first")
}

// TestNaiveEncoderFactory_GetVideoOption_AbsentReturnsNil pins the BAD
// side: a missing key reports nil so callers can use a simple nil check
// rather than passing in a default. Also verifies that Get on a
// never-allocated Dictionary returns nil instead of panicking.
func TestNaiveEncoderFactory_GetVideoOption_AbsentReturnsNil(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	assert.Nil(t, f.GetVideoOption(ctx, "pix_fmt"), "nil Dictionary case")

	require.NoError(t, f.SetVideoOption(ctx, "forced-idr", "1"))
	assert.Nil(t, f.GetVideoOption(ctx, "pix_fmt"), "allocated but key absent")
}

// TestNaiveEncoderFactory_SetVideoOptionIfAbsent_FirstWriteWins pins
// the GOOD side of the if-absent contract: the first writer of a key
// succeeds and reports wrote=true. This is the streammux late-pix_fmt-
// injection path's success case.
func TestNaiveEncoderFactory_SetVideoOptionIfAbsent_FirstWriteWins(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	wrote, err := f.SetVideoOptionIfAbsent(ctx, "pix_fmt", "nv12")
	require.NoError(t, err)
	assert.True(t, wrote, "first writer of an absent key must report wrote=true")
	got := f.GetVideoOption(ctx, "pix_fmt")
	require.NotNil(t, got)
	assert.Equal(t, "nv12", *got)
}

// TestNaiveEncoderFactory_SetVideoOptionIfAbsent_PreservesExplicit pins
// the BAD side: when the key already has a value, IfAbsent must leave
// it alone and report wrote=false. This is the operator-override
// contract for pix_fmt — if the caller explicitly set yuv420p, the late
// nv12 injection must not clobber it.
func TestNaiveEncoderFactory_SetVideoOptionIfAbsent_PreservesExplicit(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	require.NoError(t, f.SetVideoOption(ctx, "pix_fmt", "yuv420p"))

	wrote, err := f.SetVideoOptionIfAbsent(ctx, "pix_fmt", "nv12")
	require.NoError(t, err)
	assert.False(t, wrote, "if-absent must NOT overwrite explicit value")
	got := f.GetVideoOption(ctx, "pix_fmt")
	require.NotNil(t, got)
	assert.Equal(t, "yuv420p", *got, "explicit value must survive")
}

// TestNaiveEncoderFactory_SetVideoOptionIfAbsent_ConcurrentSingleWriter
// pins the atomicity claim: among N concurrent IfAbsent calls for the
// same absent key, exactly one must report wrote=true and the final
// value must be the one written by that goroutine. The previous
// non-atomic "Get then Set" pattern in the streammux helper would have
// allowed multiple writers to slip through the absence check.
func TestNaiveEncoderFactory_SetVideoOptionIfAbsent_ConcurrentSingleWriter(t *testing.T) {
	const goroutines = 32
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)

	var wg sync.WaitGroup
	wroteCh := make(chan struct {
		wrote bool
		val   string
	}, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		val := []string{"nv12", "yuv420p"}[i%2]
		go func() {
			defer wg.Done()
			wrote, err := f.SetVideoOptionIfAbsent(ctx, "pix_fmt", val)
			if err == nil {
				wroteCh <- struct {
					wrote bool
					val   string
				}{wrote, val}
			}
		}()
	}
	wg.Wait()
	close(wroteCh)

	wroteCount := 0
	var winnerVal string
	for r := range wroteCh {
		if r.wrote {
			wroteCount++
			winnerVal = r.val
		}
	}
	assert.Equal(t, 1, wroteCount, "exactly one IfAbsent caller must have written")
	final := f.GetVideoOption(ctx, "pix_fmt")
	require.NotNil(t, final)
	assert.Equal(t, winnerVal, *final,
		"final value must equal what the single writer wrote")
}
