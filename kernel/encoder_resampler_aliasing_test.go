// encoder_resampler_aliasing_test.go reproduces the slab-aliasing UAF
// where prepareResampler dropped e.ResampledFrames to GC, the per-frame
// pool finalizer (pool/pool.go:35) ran av_frame_free asynchronously,
// and a freshly-Pool.Get'd *Frame wrapping the same C slab crashed
// on Unref (prod: resampler.New -> Pool.Put -> Frame.Unref,
// addr=0xbb80==SampleRate).
//
// Strategy: subprocess-based — SIGSEGV in the child is captured as a
// non-zero exit, never killing the parent test runner. Pre-fix the
// child crashes within 30 s under format-change + slab churn + GC
// pressure. Post-fix it completes with balanced Pool counters.
//
// Determinism note: the timing-based stress is probabilistic. Pair with
// the frameaudit build-tag instrumentation in pool/frameaudit_*.go which
// turns the slab-alias into a Go-side panic before any cgo deref —
// run with `go test -tags=frameaudit ./...`.

package kernel_test

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/resampler"
	"github.com/xaionaro-go/observability"
)

const (
	bug1ChildEnv      = "FFSTREAM_BUG1_REPRO_CHILD"
	bug1StressBudget  = 30 * time.Second
	bug1ReinitMinIter = 5000
)

func TestEncoderResamplerAliasing_NoSegv(t *testing.T) {
	if os.Getenv(bug1ChildEnv) != "" {
		runChildAliasingStress(t)
		return
	}

	cmd := exec.Command(
		os.Args[0],
		"-test.run=^TestEncoderResamplerAliasing_NoSegv$",
		"-test.v",
		"-test.timeout=2m",
	)
	cmd.Env = append(os.Environ(), bug1ChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	stderr := string(out)

	require.NoErrorf(t, err,
		"child process aliasing-stress failed; stderr:\n%s", stderr)
	require.NotContains(t, stderr, "SIGSEGV", "child crashed with SIGSEGV")
	require.NotContains(t, stderr, "panic:", "child panicked")
	require.NotContains(t, stderr, "fatal error:", "child fatal-errored")
}

func runChildAliasingStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), bug1StressBudget)
	defer cancel()

	getsBefore := frame.Pool.GetCount.Load()
	putsBefore := frame.Pool.PutCount.Load()

	var (
		reinitIters atomic.Int64
		panics      atomic.Int64
	)

	formats := [2]codec.PCMAudioFormat{
		{
			SampleFormat:  astiav.SampleFormatFltp,
			SampleRate:    48000,
			ChannelLayout: astiav.ChannelLayoutStereo,
			ChunkSize:     1024,
		},
		{
			SampleFormat:  astiav.SampleFormatFltp,
			SampleRate:    44100,
			ChannelLayout: astiav.ChannelLayoutStereo,
			ChunkSize:     1024,
		},
	}

	// G1: resampler reinit driver. Each iteration constructs a Resampler
	// with one of two output formats so prepareResampler's reinit branch
	// fires (encoder.go:1058). After reinit we Close to exercise the
	// resampler.Close ResampledFrame path. We also Get/Put a few output
	// frames per iteration to populate ResampledFrames-style state.
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("G1 panicked: %v", r)
				panics.Add(1)
			}
		}()
		for i := int64(0); ; i++ {
			if ctx.Err() != nil {
				return
			}
			fmtIdx := int(i & 1)
			r, err := resampler.New(ctx, formats[fmtIdx])
			if err != nil {
				continue
			}
			out, err := r.AllocateOutputFrame(ctx)
			if err == nil {
				frame.Pool.Put(out)
			}
			_ = r.Close(ctx)
			reinitIters.Add(1)
		}
	})

	// G2: slab churn. Get/Put a fresh frame, allocate a buffer, Put back.
	// Drives slab-address recycling so freed slabs land back via fresh
	// av_frame_alloc quickly.
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("G2 panicked: %v", r)
				panics.Add(1)
			}
		}()
		layout := astiav.ChannelLayoutStereo
		for ctx.Err() == nil {
			f := frame.Pool.Get()
			f.SetSampleFormat(astiav.SampleFormatFltp)
			f.SetSampleRate(48000)
			f.SetChannelLayout(layout)
			f.SetNbSamples(1024)
			_ = f.AllocBuffer(0)
			frame.Pool.Put(f)
		}
	})

	// G3: GC pump. Two GCs in a row: the first marks finalizers ready,
	// the second runs them.
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("G3 panicked: %v", r)
				panics.Add(1)
			}
		}()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runtime.GC()
				runtime.GC()
			}
		}
	})

	<-ctx.Done()
	// Allow goroutines to observe cancellation and return.
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	runtime.GC()

	require.Zero(t, panics.Load(), "child workers panicked")
	require.GreaterOrEqualf(t, reinitIters.Load(), int64(bug1ReinitMinIter),
		"insufficient reinit iterations (%d < %d) — stress not exercised",
		reinitIters.Load(), bug1ReinitMinIter)

	gets := frame.Pool.GetCount.Load() - getsBefore
	puts := frame.Pool.PutCount.Load() - putsBefore
	require.Equalf(t, gets, puts,
		"frame.Pool leaked frames: gets=%d puts=%d delta=%d",
		gets, puts, gets-puts)
}
