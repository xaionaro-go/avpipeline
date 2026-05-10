// codec_resource_manager_test.go pins the nil-defensive contract of
// outputAsResourceManager.canReuse for cascade-init timing classes:
//
//   - params *astiav.CodecParameters is nil (Go-pointer level)
//   - EncoderFactory.VideoResolution *Resolution is nil
//
// Both originate from the same race: encoder factory init can fire reuse
// queries before its operands are fully bound. Without these guards the
// width-comparison branch in canReuse dereferences a nil pointer and
// SIGSEGVs at addr=0x0 — Resolution.Width is the first field of
// codec/types.Resolution.

package streammux

import (
	"bytes"
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	beltlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	sirupsenlogrus "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/processor"
)

// TestOutputAsResourceManager_canReuse_NilParams_DeclinesReuse asserts that
// calling canReuse with nil *astiav.CodecParameters does not panic; the
// function must conservatively return false (decline reuse) and let the
// caller fall through to creating a fresh resource pool, mirroring the
// existing decoder/CodecContext nil-guard pattern in canReuse.
func TestOutputAsResourceManager_canReuse_NilParams_DeclinesReuse(t *testing.T) {
	var rm outputAsResourceManager[any]
	ctx := context.Background()

	require.NotPanics(t, func() {
		got := rm.canReuse(ctx, true, nil, astiav.NewRational(1, 1000))
		require.False(t, got, "canReuse with nil params must decline reuse")
	})
}

// TestOutputAsResourceManager_canReuse_NilVideoResolution_DeclinesReuse
// pins the contract that canReuse must decline reuse (return false) without
// panicking when the encoder factory's VideoResolution pointer is nil.
//
// Production reproduction (Pixel 8a goal1): encoder factory cascade-init
// fires reuse queries before VideoResolution is bound, leaving the pointer
// nil when canReuse reads it. Subsequent encRes.Width access in the
// width-comparison branch dereferences the nil *Resolution at offset 0
// and panics with SIGSEGV addr=0x0.
//
// Empirical evidence: phone-log panic stack trace shows the fault inside
// canReuse's width-comparison branch with addr=0x0; disassembly confirms
// the faulting instruction is the load of encRes.Width after the nil
// VideoResolution pointer is dereferenced.
func TestOutputAsResourceManager_canReuse_NilVideoResolution_DeclinesReuse(t *testing.T) {
	o := &Output[any]{}
	o.TranscoderNode = &NodeTranscoder[OutputCustomData[any]]{
		Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
			Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
				Encoder: &kernel.Encoder[*codec.NaiveEncoderFactory]{
					EncoderFactory: &codec.NaiveEncoderFactory{
						// NaiveEncoderFactoryParams.VideoResolution
						// intentionally left nil (zero-value of
						// embedded NaiveEncoderFactoryParams.VideoResolution
						// *Resolution).
					},
				},
			},
		},
	}
	rm := o.asCodecResourceManager()
	ctx := context.Background()

	// Use a freshly allocated CodecParameters so the Go-level params
	// nil-guard is bypassed and execution reaches the VideoResolution
	// read.
	params := astiav.AllocCodecParameters()
	defer params.Free()

	// Provide a getDecoderer option so the EncoderFactoryOptionLatest
	// lookup succeeds and execution proceeds past the !ok early-return
	// to reach the encRes.Width comparison where the nil VideoResolution
	// dereference would occur without the encRes nil-guard.
	opt := codec.EncoderFactoryOptionGetDecoderer{
		GetDecoderer: nilDecoderGetter{},
	}

	require.NotPanics(t, func() {
		got := rm.canReuse(ctx, true, params, astiav.NewRational(1, 1000), opt)
		require.False(t, got, "canReuse with nil VideoResolution must decline reuse")
	})
}

// nilDecoderGetter satisfies codec.GetDecoderer with a nil decoder. The
// VideoResolution-nil test does not need a real decoder — execution must
// decline reuse before reaching the decoder.GetDecoder() call further
// down in canReuse.
type nilDecoderGetter struct{}

func (nilDecoderGetter) GetDecoder() *codec.Decoder { return nil }

// newCanReuseTestChain builds the Output -> TranscoderNode -> Processor ->
// Kernel -> Encoder -> EncoderFactory chain shared by the
// VideoResolution-nil and the positive-case Falsifiability tests
// (TestOutputAsResourceManager_canReuse_NilVideoResolution_DeclinesReuse,
// TestOutputAsResourceManager_canReuse_NonNilParams_WidthMismatch_DeclinesReuse,
// TestOutputAsResourceManager_canReuse_NonNilParams_DimensionsMatch_DeclinesOnNilDecoder).
// Pass nil for videoRes to exercise the encRes-nil guard; pass a non-nil
// *codectypes.Resolution to exercise the post-guard width/height-mismatch
// decline branches and the decoder-nil short-circuit.
func newCanReuseTestChain(videoRes *codectypes.Resolution) *outputAsResourceManager[any] {
	o := &Output[any]{}
	o.TranscoderNode = &NodeTranscoder[OutputCustomData[any]]{
		Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
			Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
				Encoder: &kernel.Encoder[*codec.NaiveEncoderFactory]{
					EncoderFactory: &codec.NaiveEncoderFactory{
						NaiveEncoderFactoryParams: codec.NaiveEncoderFactoryParams{
							VideoResolution: videoRes,
						},
					},
				},
			},
		},
	}
	return o.asCodecResourceManager()
}

// newLoggingCtx returns a context with a Trace-level logger writing into
// the returned buffer. Callers use the buffer to assert on log lines that
// witness which canReuse branch executed (e.g., the "width mismatch"
// Tracef at the width-comparison branch in canReuse). The capture pattern
// mirrors avpipeline/kernel/reorder_monotonic_dts_test.go's logging-test
// idiom.
//
// Tracef capture requires `-tags debug_trace` at go-test invocation time —
// the package-local logger.Tracef wrapper is build-tag gated (compile-time
// no-op without the tag per logger/logger_notrace.go). The package's
// canonical Makefile test target already passes the tag; running tests
// without it makes Tracef-dependent assertions silently fail at default
// build level.
//
// NOTE: not goroutine-safe; do not call t.Parallel() on tests using this
// helper. The single bytes.Buffer is shared across all log emissions in
// the test's lifetime; concurrent writes would race.
func newLoggingCtx() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	rawLogger := sirupsenlogrus.New()
	rawLogger.SetOutput(&buf)
	rawLogger.SetLevel(sirupsenlogrus.TraceLevel)
	l := beltlogrus.New(rawLogger).WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger { return l })
	return ctx, &buf
}

// TestOutputAsResourceManager_canReuse_NonNilParams_WidthMismatch_DeclinesReuse
// is T-CRIT1.3 from /tmp/claude-plans/task30-r8-test-specs.md §5: a
// positive-case Falsifiability witness for the width-mismatch decline
// branch in canReuse.
//
// What this test PROVES (Issue 1 closure per spec §1):
//
// The two existing nil-tests (TestNilParams + TestNilVideoResolution)
// prove `nil -> false` for both nil-source cases but NOTHING proves the
// post-nil-guard logic IS reached when params and VideoResolution are
// both non-nil. A miswrite that breaks the width-mismatch branch (e.g.,
// always-decline at the params nil-guard or always-decline at the encRes
// nil-guard or the comment-out of the width-mismatch branch itself)
// would still pass both existing nil-tests. Per testing-discipline
// "tests as falsification attempts — tests that cannot fail are
// worthless" — the existing suite has a Falsifiability gap on the
// post-nil-guard logic.
//
// This test closes that gap for the width-mismatch branch by:
//   - constructing the same chain as TestNilVideoResolution,
//   - setting EncoderFactory.VideoResolution to a NON-nil
//     codectypes.Resolution (640x480),
//   - allocating params with MISMATCHING width (800),
//   - asserting (a) NotPanics, (b) canReuse returns false, AND
//     (c) the "width mismatch" Tracef in canReuse's width-comparison
//     branch fires (logger-capture witness).
//
// Assertion (c) is the load-bearing witness — it fails on the A1
// (always-decline at params nil-guard), A2 (always-decline at encRes
// nil-guard), and B (comment-out width-mismatch branch) miswrite
// mutations from spec §5 broke-the-code-validation table; without
// (c), the suite would still miss those mutations because they all
// short-circuit before the width comparison and produce the same
// "false" return that (b) asserts.
//
// Broke-the-code-validation:
//   - A1 mutation (replace params nil-guard with `if true { return false }`):
//     log buffer does NOT contain "width mismatch" because the params
//     nil-guard short-circuits before the width comparison. Assertion (c)
//     FAILS; (b) still passes (false is the expected return).
//   - A2 mutation (replace encRes nil-guard with `if true { return false }`):
//     same outcome as A1 — short-circuit before width comparison.
//     Assertion (c) FAILS; (b) still passes.
//   - B mutation (comment out the width-mismatch branch in canReuse):
//     no "width mismatch" Tracef fires. Assertion (c) FAILS;
//     (b) passes by virtue of the decoder-nil short-circuit further
//     down returning false.
//   - Post-fix (current HEAD): all three assertions PASS.
//
// Empirical broke-the-code A/B logs captured in submission per
// testing-discipline "show both outputs in the report."
func TestOutputAsResourceManager_canReuse_NonNilParams_WidthMismatch_DeclinesReuse(t *testing.T) {
	rm := newCanReuseTestChain(&codectypes.Resolution{Width: 640, Height: 480})
	ctx, logBuf := newLoggingCtx()
	defer belt.Flush(ctx)

	params := astiav.AllocCodecParameters()
	defer params.Free()
	// Mismatch on width specifically (height also mismatches but the
	// width-comparison branch in canReuse evaluates first, so the
	// "width mismatch" Tracef is the witness this test asserts).
	params.SetWidth(800)
	params.SetHeight(600)

	opt := codec.EncoderFactoryOptionGetDecoderer{
		GetDecoderer: nilDecoderGetter{},
	}

	var got bool
	require.NotPanics(t, func() {
		got = rm.canReuse(ctx, true, params, astiav.NewRational(1, 1000), opt)
	}, "positive case (non-nil params + non-nil VideoResolution) must not panic")

	require.False(t, got,
		"width-mismatch must decline reuse (params=800 vs encoder VideoResolution.Width=640)")

	require.Contains(t, logBuf.String(), "width mismatch",
		"the width-comparison branch in canReuse must fire its Tracef "+
			"so the witness proves the post-nil-guard logic was reached")
}

// TestOutputAsResourceManager_canReuse_NonNilParams_DimensionsMatch_DeclinesOnNilDecoder
// is T-CRIT1.4 from /tmp/claude-plans/task30-r8-test-specs.md §6: the
// symmetric positive-match witness that exercises canReuse's
// post-width/height-match path through to the decoder-nil short-circuit.
//
// What this test PROVES (symmetric coverage closure to T-CRIT1.3):
//
// T-CRIT1.3 covers the width-mismatch decline branch. The post-match
// path through canReuse — width matches, height matches, then the
// decoder-nil short-circuit declines reuse — was NOT exercised by any
// existing test. A future regression that breaks the decoder-nil
// short-circuit (variant D in spec §6: comment out the decoder-nil
// branch) would let execution flow past the short-circuit to the
// pixel-format guard; with default-allocated params (PixelFormatNone)
// that branch short-circuits and canReuse returns true — a false
// "reuse-allowed" verdict that no existing test catches.
//
// This test closes that gap by:
//   - constructing the same chain as T-CRIT1.3,
//   - allocating params with MATCHING width (640) and height (480),
//   - asserting (a) NotPanics, (b) canReuse returns false (the
//     decoder-nil short-circuit declines reuse), AND (c) the
//     "decoder is nil" Debugf in canReuse's decoder-nil branch
//     fires (logger-capture witness).
//
// Assertion (c) is the load-bearing witness for variant D — without
// (c), the comment-out-decoder-nil mutation would return true and
// (b) would FAIL, but the failure would not pinpoint *which* branch
// regressed (could be a decoder mock returning non-nil, could be a
// pixel-format guard regression, could be the width/height match
// branches falsely declining). The Debugf witness ties the failure
// to the decoder-nil branch specifically.
//
// Broke-the-code-validation:
//   - C mutation (comment out width and height mismatch branches): test
//     still PASSES because canReuse still reaches the decoder-nil
//     short-circuit. Variant C is a no-op for this test (T-CRIT1.3
//     covers the mismatch branches; this test deliberately matches).
//   - D mutation (comment out the decoder-nil short-circuit in canReuse):
//     execution flows past the decoder-nil branch to the pixel-format
//     guard; with default-allocated params (PixelFormatNone) that
//     branch short-circuits and canReuse returns true. Assertion (b)
//     FAILS; assertion (c) FAILS (no "decoder is nil" Debugf fires).
//   - Post-fix (current HEAD): all three assertions PASS.
//
// Empirical broke-the-code A/B logs captured in submission.
func TestOutputAsResourceManager_canReuse_NonNilParams_DimensionsMatch_DeclinesOnNilDecoder(t *testing.T) {
	rm := newCanReuseTestChain(&codectypes.Resolution{Width: 640, Height: 480})
	ctx, logBuf := newLoggingCtx()
	defer belt.Flush(ctx)

	params := astiav.AllocCodecParameters()
	defer params.Free()
	// Match the encoder VideoResolution exactly so execution flows past
	// the width and height comparison branches to the decoder-nil
	// short-circuit further down in canReuse.
	params.SetWidth(640)
	params.SetHeight(480)

	opt := codec.EncoderFactoryOptionGetDecoderer{
		GetDecoderer: nilDecoderGetter{},
	}

	var got bool
	require.NotPanics(t, func() {
		got = rm.canReuse(ctx, true, params, astiav.NewRational(1, 1000), opt)
	}, "positive-match case (matching dimensions + nil decoder) must not panic")

	require.False(t, got,
		"nil decoder must decline reuse even when dimensions match "+
			"(decoder-nil short-circuit in canReuse)")

	require.Contains(t, logBuf.String(), "decoder is nil",
		"the decoder-nil branch in canReuse must fire its Debugf "+
			"so the witness proves execution flowed past the width/height "+
			"match branches to the decoder-nil short-circuit")
}
