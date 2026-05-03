// encoder_locked_hwframesctx_deadlock_test.go: regression test for the
// fb4afb2 deadlock. fb4afb2 added three call sites in kernel/encoder.go
// (fitFrameForEncoding line ~916, getScaledFrame line ~1271,
// prepareScaler line ~1420) that invoke
// streamEncoderLocked.Encoder.HardwareFramesContext(ctx) from inside the
// LockDo callback -- so the encoder.locker write-lock is already held.
//
// EncoderFullLocked did NOT override HardwareFramesContext, so the call
// fell through to the embedded *Codec.HardwareFramesContext, which calls
// xsync.DoR1(&c.locker, ...) -- a write-lock on the SAME locker. Go's
// sync.RWMutex is non-reentrant, so the goroutine deadlocks forever.
// The encoder hot path stalls; downstream queues fill; the user sees
// 0 video / 0 audio frames in the published stream.
//
// This test exercises the EXACT pattern fb4afb2 introduced -- call
// HardwareFramesContext from inside LockDo -- and asserts it returns
// within a short timeout. Without the fix the goroutine hangs and the
// test fails on the timeout. With the override on EncoderFullLocked the
// inner call resolves the method on the locked variant (no extra lock
// acquisition) and returns immediately.
//
// The test uses a SW codec (libx264 / mpeg4 fallback) -- the deadlock is
// purely a property of the locker re-entry, not of any HW-specific code
// path. HardwareFramesContext returns nil for SW encoders, but the
// failure mode (re-acquire on c.locker) is identical regardless of
// return value.

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

func newDeadlockTestEncoder(t *testing.T) codec.Encoder {
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
	t.Cleanup(func() { _ = enc.Close(ctx) })
	return enc
}

// TestEncoder_HardwareFramesContext_NoDeadlockFromLockedContext is the RED
// test for the fb4afb2 deadlock. It mirrors the call shape the kernel
// streamEncoderLocked uses: enter LockDo, then on the locked encoder
// argument invoke HardwareFramesContext.
//
// The watchdog timeout is 5s -- plenty of slack for any legitimate
// (non-deadlocked) execution on the slowest CI box; the deadlock would
// be infinite, so any reasonable upper bound separates pass from fail.
func TestEncoder_HardwareFramesContext_NoDeadlockFromLockedContext(t *testing.T) {
	t.Parallel()
	enc := newDeadlockTestEncoder(t)
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = enc.LockDo(ctx, func(ctx context.Context, inner codec.Encoder) error {
			// This is the offending call shape from fb4afb2:
			// kernel/encoder.go fitFrameForEncoding does
			//   e.Encoder.HardwareFramesContext(ctx)
			// where e.Encoder is the locked variant passed via the
			// LockDo callback. Without the override on
			// EncoderFullLocked, this re-acquires c.locker and
			// deadlocks.
			_ = inner.HardwareFramesContext(ctx)
			return nil
		})
	}()

	select {
	case <-done:
		// Reached: the call returned without deadlocking.
	case <-time.After(5 * time.Second):
		t.Fatal("HardwareFramesContext from inside LockDo deadlocked: " +
			"EncoderFullLocked must override HardwareFramesContext to avoid " +
			"re-acquiring the codec locker held by LockDo")
	}
}
