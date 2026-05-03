// leak_real_libav_test.go drives resampler.New through a *real* libav
// av_frame_get_buffer failure (not the synthetic test seam used in
// leak_test.go). The synthetic seam can't reproduce the production
// SIGSEGV mechanism because it never lets libav touch the AVFrame, so
// the subsequent pool ResetFunc -> av_frame_unref runs against a clean
// frame. This test forces the real av_frame_get_buffer call to fail
// (huge nb_samples that overflows av_samples_get_buffer_size) so that
// the failure-path cleanup runs against an AVFrame that libav has
// actually inspected/touched.
//
// Coverage gap closed: leak_test.go proved the fix returns the frame
// to the pool on synthetic AllocBuffer error; this test proves the
// pool ResetFunc (av_frame_unref) does not crash when the AllocBuffer
// failure originated from libav itself.

package resampler

import (
	"context"
	"runtime"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
)

// TestNew_RealLibavAllocBufferFailure_NoCgoCrash exercises the
// production-shaped failure: real av_frame_get_buffer fails on the
// pooled AVFrame, then resampler.New must clean up without a cgo
// SIGSEGV. The repeated iterations stress recycled-frame state by
// forcing the same pool entry through Get -> SetX -> failed
// AllocBuffer -> Put many times.
func TestNew_RealLibavAllocBufferFailure_NoCgoCrash(t *testing.T) {
	ctx := context.Background()

	prev := allocResampledFrameBuffer
	t.Cleanup(func() { allocResampledFrameBuffer = prev })

	// Force REAL libav to fail by setting nb_samples to a value that
	// overflows av_samples_get_buffer_size. validateOutputFormat sees
	// the user-supplied codec.PCMAudioFormat (still valid); the hook
	// mutates the frame after Set* calls so libav rejects the alloc.
	allocResampledFrameBuffer = func(f *astiav.Frame) error {
		f.SetNbSamples(1 << 30)
		return f.AllocBuffer(0)
	}

	cfg := codec.PCMAudioFormat{
		SampleFormat:  astiav.SampleFormatFltp,
		SampleRate:    48000,
		ChannelLayout: astiav.ChannelLayoutStereo,
		ChunkSize:     1024,
	}

	getsBefore := frame.Pool.GetCount.Load()
	putsBefore := frame.Pool.PutCount.Load()

	const iterations = 50
	for i := 0; i < iterations; i++ {
		r, err := New(ctx, cfg)
		require.Error(t, err, "iter %d: New must fail", i)
		require.Nil(t, r, "iter %d", i)
		require.Contains(t, err.Error(), "alloc buffer for resampled frame")
	}

	gets := frame.Pool.GetCount.Load() - getsBefore
	puts := frame.Pool.PutCount.Load() - putsBefore
	require.Equal(t, int64(iterations), gets)
	require.Equal(t, gets, puts,
		"every Pool.Get must be matched by a Pool.Put (gets=%d, puts=%d)", gets, puts)

	// Trigger any pending finalizers on leaked frames. With a correct
	// fix this is a no-op; with a regression where leaked frames carry
	// the pool finalizer, GC firing here would invoke av_frame_free on
	// the partial-state AVFrame and crash inside libav.
	runtime.GC()
	runtime.GC()
}
