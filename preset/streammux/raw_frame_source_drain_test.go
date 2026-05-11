// raw_frame_source_drain_test.go pins the IN-FLIGHT FRAME DRAIN
// contract on SetRawFrameSource: when the flag flips from false to
// true and the per-Output retroactive ResetHard fires, the upstream
// feeder chain (InputFilter → InputFixer.AutoHeaders →
// InputFixer.MapStreamIndices → TranscoderNode) MUST also be drained
// so any rtmp-era decoded frames sitting in the queues are discarded
// before the freshly-reopened nv12 encoder sees its first frame.
//
// Without the drain, the first stale mediacodec-Surface frame either
// silent-consumes against the new nv12 encoder (no error → no
// eviction → no recreate, the silent-stall path) OR errors with
// ENOSYS and triggers the no-sibling recreate path. The drain
// eliminates both transitions; the recreate path stays as
// belt-and-suspenders for permanent faults.
//
// The observable signal used here is AutoHeaders.IsSet. AutoHeaders
// is the kernel that wraps SendingFixer.GetPacketSink() inside the
// Output; its Reset() method clears IsSet+SelectedKernel+CallCount
// (preset/autoheaders/kernel.go:73-89). FromKernel.Reset is the SSOT
// drain primitive (processor/from_kernel.go:418-430) — it
// non-blockingly drains InputCh / preOutputCh / OutputCh AND forwards
// Reset to the wrapped kernel's Resetter. So if drainUpstreamFeeders
// ForOutput correctly calls Reset on
// InputFixer.AutoHeadersNode.Processor, AutoHeaders.IsSet flips false
// — observable, deterministic, and free of timing on Serve goroutines.

package streammux

import (
	"testing"

	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// getAutoHeadersHandler unwraps the concrete *autoheaders.AutoHeaders
// from the Output's InputFixer. AutoFixer's Processor.Kernel is typed
// as kerneltypes.Abstract (interface), so reaching the concrete
// *BaseWithFormatContext wrapper requires a type assertion. Centralised
// here so the test bodies stay focused on the behavioural contract.
func getAutoHeadersHandler(t *testing.T, out *Output[struct{}]) *autoheaders.AutoHeaders {
	t.Helper()
	require.NotNil(t, out.InputFixer, "test setup: InputFixer non-nil for non-copy senderKey")
	require.NotNil(t, out.InputFixer.AutoHeadersNode, "test setup: AutoHeadersNode non-nil")
	wrapper, ok := out.InputFixer.AutoHeadersNode.Processor.Kernel.(*boilerplate.BaseWithFormatContext[*autoheaders.AutoHeaders])
	require.True(t, ok, "test setup: AutoHeadersNode kernel is *BaseWithFormatContext[*AutoHeaders]; got %T",
		out.InputFixer.AutoHeadersNode.Processor.Kernel)
	return wrapper.Handler
}

// TestSetRawFrameSource_DrainsAutoHeadersStateOnExistingOutput is the
// GOOD-side proof: when SetRawFrameSource(true) latches the flag and
// triggers a per-Output factory mutation + ResetHard, the drain helper
// MUST reach AutoHeaders.Reset and clear its IsSet state. Without the
// drain helper this assertion fails: AutoHeaders.IsSet stays true
// because nothing else in the SetRawFrameSource path touches it.
//
// Falsification protocol: comment out the
// drainUpstreamFeedersForOutput(ctx, output) call inside the
// s.Outputs.Range body in raw_frame_source_pixfmt.go and rerun this
// test — it MUST fail at the IsSet==false assertion.
func TestSetRawFrameSource_DrainsAutoHeadersStateOnExistingOutput(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity: default false")

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.NotNil(t, out.InputFixer, "test setup: InputFixer non-nil for non-copy senderKey")
	require.NotNil(t, out.InputFixer.AutoHeadersNode, "test setup: AutoHeadersNode non-nil")

	// Flip the encoderFactory's HardwareDeviceType to MediaCodec so the
	// SetRawFrameSource path actually mutates the factory and runs
	// ResetHard + drain. Without MediaCodec the
	// applyRawFrameSourceMediaCodecPixFmtToFactory check returns false
	// and the Range body returns early.
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	// Pre-condition: simulate the rtmp-era state where AutoHeaders has
	// already run sendInputLocked once and latched IsSet=true. The
	// drain MUST clear this — otherwise the freshly-opened nv12
	// encoder branch downstream would inherit detection state observed
	// against the prior connection.
	autoHeaders := getAutoHeadersHandler(t, out)
	autoHeaders.Locker.Do(ctx, func() {
		autoHeaders.IsSet = true
		autoHeaders.CallCount.Store(7) // arbitrary non-zero
	})
	require.True(t, autoHeaders.IsSet, "sanity: pre-drain IsSet must be true")

	// Trigger the production sequence: hot-add raw-frame source flips
	// the StreamMux flag from false → true, which retroactively patches
	// every Output (just this one in the test), runs ResetHard, and
	// MUST drain the upstream feeder chain.
	mux.SetRawFrameSource(ctx, true)

	// GOOD-side: the drain reached AutoHeaders.Reset.
	autoHeaders.Locker.Do(ctx, func() {
		require.False(t, autoHeaders.IsSet,
			"SetRawFrameSource(true) MUST drain InputFixer.AutoHeaders so IsSet flips back to false")
		require.Zero(t, autoHeaders.CallCount.Load(),
			"SetRawFrameSource(true) MUST drain InputFixer.AutoHeaders so CallCount resets to zero")
	})

	// GOOD-side cross-check: the per-Output flag and the StreamMux flag
	// both latched true, confirming this test exercised the same
	// SetRawFrameSource code path as the live production trigger.
	require.True(t, mux.RawFrameSource.Load(), "sanity: StreamMux flag latched true")
	require.True(t, out.RawFrameSource.Load(), "sanity: Output flag latched true")
}

// TestSetRawFrameSource_NoDrainOnNonMediaCodec is the BAD-side: when
// the encoder is not MediaCodec the factory mutation precondition
// fails inside applyRawFrameSourceMediaCodecPixFmtToFactory and the
// Range body returns early — the drain helper MUST NOT run on those
// outputs. Otherwise an unrelated CUDA / VAAPI Output would gratuitously
// have its AutoHeaders detection state cleared on every camera-tap
// even though no encoder reset happened.
func TestSetRawFrameSource_NoDrainOnNonMediaCodec(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "h264_nvenc",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeCUDA

	autoHeaders := getAutoHeadersHandler(t, out)
	autoHeaders.Locker.Do(ctx, func() {
		autoHeaders.IsSet = true
		autoHeaders.CallCount.Store(7)
	})
	require.True(t, autoHeaders.IsSet)

	mux.SetRawFrameSource(ctx, true)

	// BAD-side: no factory mutation → no encoder reset → no drain. The
	// AutoHeaders state observed against this CUDA encoder must be
	// preserved.
	autoHeaders.Locker.Do(ctx, func() {
		require.True(t, autoHeaders.IsSet,
			"non-MediaCodec output MUST NOT have its AutoHeaders state drained on SetRawFrameSource(true)")
		require.Equal(t, uint64(7), autoHeaders.CallCount.Load(),
			"non-MediaCodec output MUST preserve AutoHeaders.CallCount on SetRawFrameSource(true)")
	})
}

// TestSetRawFrameSource_FalseCallSkipsDrain is the BAD-side for the
// sticky-true contract: a SetRawFrameSource(false) call must early-
// return without running the per-Output Range body — and therefore
// without invoking the drain helper. This guards against a regression
// where a stray false call on the post-tap path would gratuitously
// drain a queue that has been legitimately re-populated.
func TestSetRawFrameSource_FalseCallSkipsDrain(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	autoHeaders := getAutoHeadersHandler(t, out)
	autoHeaders.Locker.Do(ctx, func() {
		autoHeaders.IsSet = true
		autoHeaders.CallCount.Store(3)
	})

	// false call from initial false state: must early-return per the
	// sticky-true contract documented on SetRawFrameSource.
	mux.SetRawFrameSource(ctx, false)

	autoHeaders.Locker.Do(ctx, func() {
		require.True(t, autoHeaders.IsSet,
			"SetRawFrameSource(false) MUST NOT trigger the drain helper")
		require.Equal(t, uint64(3), autoHeaders.CallCount.Load(),
			"SetRawFrameSource(false) MUST NOT trigger the drain helper")
	})
}

// TestSetRawFrameSource_SecondTrueCallIsNoOp is the BAD-side for the
// idempotency contract: once the StreamMux RawFrameSource flag has
// been latched true, a subsequent SetRawFrameSource(true) call must
// take the early-return branch (s.RawFrameSource.Set() returns true)
// and MUST NOT re-run the per-Output Range body. This protects against
// a regression where redundant calls would re-drain in-flight frames
// that have legitimately re-accumulated post-tap.
func TestSetRawFrameSource_SecondTrueCallIsNoOp(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	// First call: latches flag true, drains AutoHeaders (false→true
	// transition).
	mux.SetRawFrameSource(ctx, true)
	require.True(t, mux.RawFrameSource.Load())

	// Re-populate AutoHeaders state simulating post-drain accumulation
	// from the camera path.
	autoHeaders := getAutoHeadersHandler(t, out)
	autoHeaders.Locker.Do(ctx, func() {
		autoHeaders.IsSet = true
		autoHeaders.CallCount.Store(11)
	})

	// Second call: must be a no-op (Set() returns true → early return).
	mux.SetRawFrameSource(ctx, true)

	autoHeaders.Locker.Do(ctx, func() {
		require.True(t, autoHeaders.IsSet,
			"redundant SetRawFrameSource(true) MUST NOT re-trigger the drain helper")
		require.Equal(t, uint64(11), autoHeaders.CallCount.Load(),
			"redundant SetRawFrameSource(true) MUST NOT re-trigger the drain helper")
	})
}

// TestDrainUpstreamFeedersForOutput_ToleratesNilSubNodes pins the
// nil-tolerance contract documented on drainUpstreamFeedersForOutput.
// The InputFixer's AutoHeadersNode is currently always set (per the
// "TODO: make a.AutoHeadersNode always non-nil" comment in
// preset/autofix/auto_fix.go), but the helper's design assumes a
// future refactor may relax that. Constructing an Output with the
// AutoHeadersNode field zeroed out and exercising the helper directly
// proves the nil-guard branches do not panic.
func TestDrainUpstreamFeedersForOutput_ToleratesNilSubNodes(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.NotNil(t, out.InputFixer)

	// Synthetically zero the AutoHeadersNode so the helper exercises
	// the nil-branch. Production code today never produces this state,
	// but the helper's nil-guard MUST hold against future refactors
	// (the alternative — panic on nil — would be a latent regression
	// trigger).
	out.InputFixer.AutoHeadersNode = nil

	// Must not panic. The TranscoderNode + InputFilter Reset still
	// runs; the AutoHeadersNode + MapStreamIndices entries that are
	// nil-guarded simply skip.
	require.NotPanics(t, func() {
		drainUpstreamFeedersForOutput(ctx, out)
	}, "drainUpstreamFeedersForOutput MUST handle nil AutoHeadersNode without panicking")
}
