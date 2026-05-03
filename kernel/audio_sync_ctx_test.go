// audio_sync_ctx_test.go covers the ctx-escape backstop on outCh
// sends. Without it, SendInput would hold s.locker forever on a
// back-pressured outCh, wedging the AudioSync kernel and every node
// downstream of it.

package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	testifyrequire "github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// createTestVideoFrame mirrors createTestAudioFrame but with
// MediaTypeVideo so it hits the non-audio early-return branch
// in SendInput.
func createTestVideoFrame(streamIndex int, pts int64) packetorframe.InputUnion {
	f := astiav.AllocFrame()
	f.SetPts(pts)

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)

	return packetorframe.InputUnion{
		Frame: &frame.Input{
			Frame: f,
			StreamInfo: &frame.StreamInfo{
				StreamIndex:     streamIndex,
				TimeBase:        astiav.NewRational(1, 90000),
				CodecParameters: cp,
			},
		},
	}
}

// TestAudioSync_SendInput_ContextCanceled_NonAudio asserts that a
// non-audio frame routed through SendInput unblocks via ctx.Done()
// when outCh has no reader. Pre-fix: this test would hang
// indefinitely (the test runner timeout would fire). Post-fix:
// SendInput returns ctx.Err() within a few ms of cancellation.
//
// Determinism: cancel BEFORE launching the goroutine. SendInput's
// outCh select still observes ctx.Done() and returns ctx.Err() — the
// ctx-escape contract holds whether the cancel happens before or
// after the goroutine reaches the blocking send. The prior
// time.Sleep(20ms)-then-cancel pattern was timing-dependent (under
// load on CI -race the 20ms could elapse before the goroutine had
// reached the send, leaving cancel observable only after a
// runtime.Gosched eventually scheduled the goroutine). The pre-cancel
// pattern eliminates that window without weakening the assertion.
func TestAudioSync_SendInput_ContextCanceled_NonAudio(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	k := NewAudioSync(context.Background(), DefaultAudioSyncConfig())
	// Unbuffered channel with no reader => send blocks forever
	// without the ctx-escape.
	outCh := make(chan packetorframe.OutputUnion)

	errCh := make(chan error, 1)
	go func() {
		errCh <- k.SendInput(ctx, createTestVideoFrame(0, 0), outCh)
	}()

	select {
	case err := <-errCh:
		testifyrequire.Error(t, err, "SendInput must surface ctx.Err() on cancel")
		testifyassert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendInput did not return after ctx cancel; ctx-escape regression")
	}
}

// TestAudioSync_SendInput_ContextCanceled_AudioNoTrack asserts the
// same behaviour for an audio frame on a stream with no track config.
// This routes through the second outCh send (the "isReference == false"
// branch). Cancel-before-launch eliminates the prior 20ms sleep race
// (see _NonAudio doc above for the rationale).
func TestAudioSync_SendInput_ContextCanceled_AudioNoTrack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := DefaultAudioSyncConfig()
	// No tracks configured => state == nil and no other track
	// references stream 0 => the !isReference branch fires.
	k := NewAudioSync(context.Background(), cfg)
	outCh := make(chan packetorframe.OutputUnion)

	errCh := make(chan error, 1)
	go func() {
		errCh <- k.SendInput(
			ctx,
			createTestAudioFrame(0, 0, 48000, make([]float64, 1024)),
			outCh,
		)
	}()

	select {
	case err := <-errCh:
		testifyrequire.Error(t, err)
		testifyassert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendInput did not return after ctx cancel; ctx-escape regression")
	}
}

// TestAudioSync_SendInput_ContextCanceled_AudioReference covers the
// outCh send at the bottom of SendInput where state == nil but the
// stream serves as a reference for some other track. This exercises
// the third outCh code path. Cancel-before-launch (see _NonAudio).
func TestAudioSync_SendInput_ContextCanceled_AudioReference(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := DefaultAudioSyncConfig()
	// Track 1 references stream 0 => stream 0 is a reference
	// but has no Track config of its own (state == nil for it).
	cfg.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}
	k := NewAudioSync(context.Background(), cfg)
	outCh := make(chan packetorframe.OutputUnion)

	errCh := make(chan error, 1)
	go func() {
		errCh <- k.SendInput(
			ctx,
			createTestAudioFrame(0, 0, 48000, make([]float64, 1024)),
			outCh,
		)
	}()

	select {
	case err := <-errCh:
		testifyrequire.Error(t, err)
		testifyassert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendInput did not return after ctx cancel; ctx-escape regression")
	}
}

// TestAudioSync_SendInput_HappyPath_NonAudio confirms the ctx-escape
// does NOT break the normal-flow case: a non-audio frame with a live
// reader on outCh must still be forwarded and SendInput returns nil.
// Dual-sided: paired with the cancellation tests above.
func TestAudioSync_SendInput_HappyPath_NonAudio(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, DefaultAudioSyncConfig())
	outCh := make(chan packetorframe.OutputUnion, 1)

	err := k.SendInput(ctx, createTestVideoFrame(0, 0), outCh)
	testifyrequire.NoError(t, err)

	select {
	case got := <-outCh:
		testifyassert.NotNil(t, got.Frame)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected forwarded video frame on outCh")
	}
}
