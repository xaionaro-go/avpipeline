// encoder_reset_hard_deadlock_test.go: regression test for the
// self-deadlock in kernel.Encoder.resetHard.
//
// resetHard iterates e.encoders and for each entry:
//
//	encoder.Encoder.LockDo(ctx, func(ctx, _ codec.Encoder) error {
//	    if err := encoder.Close(ctx); err != nil { ... }
//	    delete(...); return nil
//	})
//
// codec.EncoderFull.LockDo acquires e.locker (xsync.Mutex / sync.RWMutex
// write-lock). Inside the callback, the original code called
// streamEncoder.Close which dispatches to EncoderFull.Close which calls
// withLocked which tries to re-acquire the SAME e.locker. sync.RWMutex
// is non-reentrant, so the goroutine deadlocks forever. The fix routes
// the close through the locked-variant argument supplied by LockDo (a
// *codec.EncoderFullLocked whose Close does not re-acquire the locker).
//
// This test exercises the EXACT pattern: open a real encoder, push it
// into Encoder.encoders so it survives ResetHard's iteration, call
// ResetHard with a watchdog timeout, assert it returns within the
// budget. Without the fix the goroutine hangs and the test fails on
// the timeout. With the fix the close runs through EncoderFullLocked
// (no re-lock) and ResetHard returns immediately.
//
// Determinism: SW codec only (libx264 / mpeg4 fallback). The deadlock
// is a property of the locker, not of any HW-specific path.

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

// resetHardNoopFactory is a minimal EncoderFactory that satisfies the
// interface contract for the deadlock regression test. NewEncoder is
// never called by ResetHard; Reset is invoked once at the end and just
// returns nil. We use this instead of nil to avoid an interface-call on
// a nil EncoderFactory at the post-iteration EncoderFactory.Reset call.
type resetHardNoopFactory struct{}

func (resetHardNoopFactory) String() string { return "resetHardNoopFactory" }
func (resetHardNoopFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codec.Option,
) (codec.Encoder, error) {
	return nil, nil
}
func (resetHardNoopFactory) Reset(ctx context.Context) error { return nil }

func newResetHardDeadlockEncoder(t *testing.T) codec.Encoder {
	t.Helper()
	ctx := context.Background()
	encoderCodec := astiav.FindEncoderByName("libx264")
	if encoderCodec == nil {
		encoderCodec = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoderCodec, "no libx264 / mpeg4 encoder available for the test")
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(encoderCodec.ID())
	cp.SetWidth(64)
	cp.SetHeight(64)
	enc, err := codec.NewEncoder(ctx, codec.CodecParams{
		CodecName:       codec.Name(encoderCodec.Name()),
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	return enc
}

// TestEncoder_ResetHard_NoSelfDeadlock is the RED test. It
// constructs an Encoder kernel populated with one streamEncoder backed
// by a real codec.EncoderFull, then calls ResetHard. Without the fix the
// inner LockDo callback's streamEncoder.Close re-acquires
// codec.EncoderFull.locker and the goroutine hangs forever; with the fix
// closeLocked routes the close through the locked variant and ResetHard
// returns within the watchdog budget.
//
// The watchdog is 5s -- generous slack for the slowest CI box; the
// deadlock is infinite, so any reasonable upper bound separates pass
// from fail.
func TestEncoder_ResetHard_NoSelfDeadlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	enc := newResetHardDeadlockEncoder(t)

	// Build an Encoder kernel manually with a single streamEncoder. We
	// don't need the full pipeline -- ResetHard's contract is "iterate
	// e.encoders and close each", and that's the deadlock surface.
	e := NewEncoder[codec.EncoderFactory](ctx, resetHardNoopFactory{}, nil)
	e.encoders[0] = &streamEncoder{
		Encoder:       enc,
		EncoderConfig: &e.EncoderConfig,
	}

	done := make(chan error, 1)
	go func() {
		done <- e.ResetHard(ctx)
	}()

	select {
	case err := <-done:
		// Reached: ResetHard returned without deadlocking. The bug under
		// test is the deadlock, not the error path; the noop factory
		// guarantees the post-iteration factory.Reset returns nil so any
		// non-nil err here would indicate an unrelated regression.
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Encoder.ResetHard self-deadlocked: " +
			"streamEncoder.Close called from inside LockDo re-acquires " +
			"codec.EncoderFull.locker (already held). The fix routes " +
			"the close through the locked variant supplied by LockDo.")
	}
}
