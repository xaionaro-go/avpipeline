// encoder_codec_params_republish_test.go covers Bug 6.2 (alt fix):
// after reinitEncoderForResamplerRebuild advances InitTS (and resets
// LastInitTS), the next encoded packet's drain pass MUST refresh the
// downstream-visible outputStream.CodecParameters() with the freshly-
// regenerated extradata (AAC ASC). This test asserts that contract on
// the helper republishCodecParamsIfStale, which is what the drain
// callback calls in lieu of the old (deadlocking) pre-drain block at
// the encoder hot path.
//
// The prior fix (commit 9f07679) re-published codec params before
// drain, but the call site was inside `streamEncoder.Encoder.LockDo`
// and re-entered the same encoder lock via
// `streamEncoder.Encoder.ToCodecParameters` -- a non-reentrant
// xsync.RWMutex deadlock. Production AAC encoders (EncoderFull) thus
// stalled on the codec lock; queues filled, route consumer-detach
// fired, audio dropped. Reverted in commit e99df5e.
//
// The alt fix: republish from inside the drain callback where the
// encoder is already locked (drain receives *codec.EncoderFullLocked).
// The helper takes a *streamEncoder and the locked codec context, so
// it can be unit-tested without spinning up a real AAC encoder.
//
// Determinism: this test exercises pure state transitions on a
// streamEncoder + an astiav.Stream + a fake getInitTS. No goroutines,
// no real encoder, no libav decode/encode round-trip.
package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

// TestRepublishCodecParamsIfStale_FiresWhenInitTSAdvances is the RED
// test for the alt fix: when LastInitTS is older than the encoder's
// InitTS, republishCodecParamsIfStale must invoke the writer (which
// emulates cc.ToCodecParameters) and bump LastInitTS.
func TestRepublishCodecParamsIfStale_FiresWhenInitTSAdvances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	se.LastInitTS = time.Time{} // freshly reset by reinitEncoderForResamplerRebuild
	newInitTS := time.Unix(1700000000, 0)

	// Output stream we will assert is touched.
	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	writeCalls := 0
	writer := func(*astiav.CodecParameters) error {
		writeCalls++
		return nil
	}

	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)

	require.Equal(t, 1, writeCalls,
		"writer must be called exactly once when LastInitTS is older than the encoder InitTS")
	require.Equal(t, newInitTS, se.LastInitTS,
		"LastInitTS must advance to the encoder InitTS after a successful republish")
}

// TestRepublishCodecParamsIfStale_NoOpWhenLastInitTSCurrent ensures the
// helper does not re-publish on every drain when nothing changed --
// republishing on every packet would churn outputStream's codec
// parameters and waste CPU.
func TestRepublishCodecParamsIfStale_NoOpWhenLastInitTSCurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	currentInitTS := time.Unix(1700000000, 0)
	se := &streamEncoder{LastInitTS: currentInitTS}

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	writeCalls := 0
	writer := func(*astiav.CodecParameters) error {
		writeCalls++
		return nil
	}

	republishCodecParamsIfStale(ctx, se, outputStream, currentInitTS, writer)

	require.Equal(t, 0, writeCalls,
		"writer must not be called when LastInitTS already equals the encoder InitTS")
	require.Equal(t, currentInitTS, se.LastInitTS,
		"LastInitTS must remain unchanged on the no-op path")
}

// TestRepublishCodecParamsIfStale_NoDeadlockOnReentrantLock is a
// regression guard for the original bug: the pre-fix code lived inside
// streamEncoder.Encoder.LockDo and called streamEncoder.Encoder.
// ToCodecParameters, which re-acquired the same non-reentrant lock and
// deadlocked. The alt fix runs the publish from a context where the
// encoder is *already* locked (drain callback) and uses a writer that
// does NOT take any encoder-level lock. This test simulates the
// invariant: the writer passed to republishCodecParamsIfStale must be
// invoked synchronously and return without touching the caller's lock.
func TestRepublishCodecParamsIfStale_NoDeadlockOnReentrantLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	newInitTS := time.Unix(1700000000, 0)

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	// The writer is invoked while the caller already holds the
	// encoder lock; if the helper tried to re-acquire that lock the
	// caller would deadlock. The bounded done channel simulates the
	// "no nested lock" expectation: the helper must finish in-band.
	done := make(chan struct{})
	writer := func(*astiav.CodecParameters) error {
		close(done)
		return nil
	}

	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)

	select {
	case <-done:
		// happy path: writer ran in-band
	default:
		t.Fatal("republishCodecParamsIfStale must invoke the writer synchronously (no goroutine, no deferred lock acquisition)")
	}
}

// TestRepublishCodecParamsIfStale_WriterErrorRetriesNextPacket asserts
// that a failing writer leaves LastInitTS unchanged so the next drained
// packet retries the publish. This pins the retry contract that the
// warn-backoff (TestRepublishCodecParamsIfStale_WarnBackoffOnRepeatedFailure
// below) is built on.
func TestRepublishCodecParamsIfStale_WriterErrorRetriesNextPacket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	newInitTS := time.Unix(1700000000, 0)

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	calls := 0
	writer := func(*astiav.CodecParameters) error {
		calls++
		return errFakeWriter
	}

	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)
	require.Equal(t, 1, calls, "first call must invoke writer")
	require.True(t, se.LastInitTS.IsZero(),
		"LastInitTS must NOT advance on writer failure (retry semantics)")

	// Second invocation with same encoderInitTS — writer must be retried.
	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)
	require.Equal(t, 2, calls, "second call must retry writer")
	require.True(t, se.LastInitTS.IsZero(),
		"LastInitTS still must not advance on persistent failure")
}

// TestRepublishCodecParamsIfStale_WarnBackoffOnRepeatedFailure pins the
// warn-spam backoff contract added in the aspect-C unified fixer: when
// the writer keeps failing across many drained packets (production hot
// path: drain runs at audio frame rate, ~50fps for 1024-sample AAC at
// 48kHz), the Warnf must NOT fire on every packet. The helper rate-
// limits the warn via streamEncoder.lastCodecParamsRepublishWarnAt;
// successive failures within codecParamsRepublishWarnBackoff are
// downgraded to Debug.
//
// The test exercises the rate-limit purely on internal state (the
// stamp moves forward only when a Warnf fires), without depending on
// real wall-time advancement: invoke twice in quick succession with
// the same writer error, then verify the second invocation did NOT
// shift the stamp (because the backoff window had not elapsed).
//
// Falsification: removing the backoff branch (so every failure fires
// Warnf and stamps the time) makes the second-invocation assertion
// fail because lastCodecParamsRepublishWarnAt would have been
// re-stamped at the second call.
func TestRepublishCodecParamsIfStale_WarnBackoffOnRepeatedFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{}
	newInitTS := time.Unix(1700000000, 0)

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	writer := func(*astiav.CodecParameters) error { return errFakeWriter }

	// First failure: stamps the warn time.
	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)
	firstStamp := se.lastCodecParamsRepublishWarnAt
	require.False(t, firstStamp.IsZero(),
		"first failure must record the warn time so subsequent failures can be backoff-suppressed")

	// Second failure within the backoff window: must NOT update the
	// stamp (which would happen if Warnf fired again). The helper
	// either keeps the same stamp (suppressed) or advances it
	// (Warnf fired). The assertion catches the latter.
	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)
	require.Equal(t, firstStamp, se.lastCodecParamsRepublishWarnAt,
		"second failure within backoff window must keep stamp unchanged (Warnf suppressed)")

	// LastInitTS must still be zero — failures did not advance it,
	// so the next-packet-retries contract is preserved alongside the
	// backoff.
	require.True(t, se.LastInitTS.IsZero(),
		"backoff must not paper over the retry contract: LastInitTS stays zero on persistent failure")
}

// TestRepublishCodecParamsIfStale_BackoffStampClearedOnSuccess pins the
// recovery side of the backoff: a successful republish must clear
// lastCodecParamsRepublishWarnAt so a NEW failure window (after the
// next reinit advances encoderInitTS) is observed without waiting out
// a leftover backoff from a previous run.
func TestRepublishCodecParamsIfStale_BackoffStampClearedOnSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	se := &streamEncoder{
		lastCodecParamsRepublishWarnAt: time.Now(), // pretend a prior failure stamped this
	}
	newInitTS := time.Unix(1700000000, 0)

	fmtCtx := astiav.AllocFormatContext()
	defer fmtCtx.Free()
	outputStream := fmtCtx.NewStream(nil)
	require.NotNil(t, outputStream)

	writer := func(*astiav.CodecParameters) error { return nil } // success

	republishCodecParamsIfStale(ctx, se, outputStream, newInitTS, writer)

	require.True(t, se.lastCodecParamsRepublishWarnAt.IsZero(),
		"successful republish must clear the warn-backoff stamp so future failures are not silently swallowed by stale backoff")
	require.Equal(t, newInitTS, se.LastInitTS,
		"successful republish still advances LastInitTS")
}

// errFakeWriter is the canned error returned by writers in the
// failure-branch tests. Local sentinel so the assertion does not
// match unrelated errors.
var errFakeWriter = errFakeWriterError{}

type errFakeWriterError struct{}

func (errFakeWriterError) Error() string { return "fake writer error" }
