// leak_test.go covers the regression where resampler.New leaked an
// astiav.Frame on AllocBuffer failure. The leaked frame still carried
// its pool finalizer; once GC ran the finalizer it called av_frame_free
// on a frame whose internal buf[] state was partially populated by a
// failed av_frame_get_buffer, causing a SIGSEGV inside libav (observed
// in production: PC inside av_frame_free, addr=0xbb80, signal during
// cgo execution, stack from runtime.runFinalizers ->
// avpipeline/frame.init.func2 -> astiav.(*Frame).Free).

package resampler

import (
	"context"
	"errors"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
)

// TestNew_AllocBufferFailure_FramePutBackInPool drives New() onto the
// AllocBuffer-failure code path by forcing the package's allocBuffer
// hook to return EINVAL after the frame has been Got from the pool.
// The test reads the pool's Get/Put counters across the call.
//
// Fix invariant: every Pool.Get must be matched by a Pool.Put even
// when New returns an error. Without the fix, the failed frame is
// dropped to GC unmatched; its pool finalizer eventually calls
// av_frame_free on a partially-allocated AVFrame, crashing inside
// libav.
func TestNew_AllocBufferFailure_FramePutBackInPool(t *testing.T) {
	ctx := context.Background()

	// Force AllocBuffer to fail. Restore on cleanup so other tests
	// (including TestNew_ValidConfig_*) see the production behaviour.
	prev := allocResampledFrameBuffer
	t.Cleanup(func() { allocResampledFrameBuffer = prev })
	wantErr := errors.New("test-induced AllocBuffer failure")
	allocResampledFrameBuffer = func(*astiav.Frame) error { return wantErr }

	getsBefore := frame.Pool.GetCount.Load()
	putsBefore := frame.Pool.PutCount.Load()

	cfg := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}
	r, err := New(ctx, cfg)
	require.Error(t, err, "New must return an error when AllocBuffer fails")
	require.Nil(t, r)
	require.ErrorIs(t, err, wantErr)
	require.Contains(t, err.Error(), "alloc buffer for resampled frame")

	getsAfter := frame.Pool.GetCount.Load()
	putsAfter := frame.Pool.PutCount.Load()

	gets := getsAfter - getsBefore
	puts := putsAfter - putsBefore
	require.Equal(t, int64(1), gets, "New must Get exactly one frame from the pool")
	require.Equal(t, gets, puts,
		"New must Put back every frame it Got even on AllocBuffer failure (gets=%d, puts=%d)",
		gets, puts)
}

// TestNew_ValidConfig_GetMatchesNewSuccess documents the success
// path's pool accounting: New() takes exactly one frame from the pool
// and the resampler holds it until its finalizer-driven cleanup. The
// failure-path test above asserts the failure mirror.
func TestNew_ValidConfig_GetMatchesNewSuccess(t *testing.T) {
	ctx := context.Background()

	getsBefore := frame.Pool.GetCount.Load()

	r, err := New(ctx, codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })

	getsAfter := frame.Pool.GetCount.Load()
	require.Equal(t, int64(1), getsAfter-getsBefore, "New takes exactly one frame from the pool")
}
