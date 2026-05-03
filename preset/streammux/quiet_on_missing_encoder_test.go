// quiet_on_missing_encoder_test.go pins the flag-gated demotion of the
// "unable to get encoder" log spam emitted by AutoBitRateHandler when
// no input is flowing yet (so no encoder is bound). Default
// (StreamMux.QuietOnMissingEncoder=false) keeps the legacy Warn level;
// when the flag is set the message demotes to Debug.

package streammux

import (
	"context"
	"sync"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/stretchr/testify/require"
)

type recordingHook struct {
	mu      sync.Mutex
	entries []loggertypes.Entry
}

var _ loggertypes.Hook = (*recordingHook)(nil)

func (h *recordingHook) ProcessLogEntry(e *loggertypes.Entry) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, *e)
	return true
}

func (h *recordingHook) Flush() {}

func (h *recordingHook) snapshot() []loggertypes.Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]loggertypes.Entry, len(h.entries))
	copy(out, h.entries)
	return out
}

func ctxWithRecordingHook(t *testing.T) (context.Context, *recordingHook) {
	t.Helper()
	hook := &recordingHook{}
	l := logrus.Default().WithLevel(logger.LevelTrace).WithHooks(hook)
	return logger.CtxWithLogger(context.Background(), l), hook
}

func levelsForMissingEncoder(entries []loggertypes.Entry) []logger.Level {
	var out []logger.Level
	for _, e := range entries {
		if e.Message == "unable to get encoder" {
			out = append(out, e.Level)
		}
	}
	return out
}

// TestAutoBitRateHandler_LogMissingEncoder_Default pins the legacy
// behavior: when StreamMux.QuietOnMissingEncoder is false (default), the
// helper emits the message at Warn so existing diagnostics are not
// lost.
func TestAutoBitRateHandler_LogMissingEncoder_Default(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	sm := &StreamMux[struct{}]{}
	require.False(t, sm.QuietOnMissingEncoder.Load(),
		"sanity: zero-value StreamMux must have QuietOnMissingEncoder=false")

	h := &AutoBitRateHandler[struct{}]{StreamMux: sm}
	h.logMissingEncoder(ctx)

	levels := levelsForMissingEncoder(hook.snapshot())
	require.Equal(t, []logger.Level{logger.LevelWarning}, levels,
		"default (QuietOnMissingEncoder=false) must keep Warn level; got %v", levels)
}

// TestAutoBitRateHandler_LogMissingEncoder_QuietDemotesToDebug pins
// the flag-gated demotion: when StreamMux.QuietOnMissingEncoder is true,
// the helper emits the message at Debug.
func TestAutoBitRateHandler_LogMissingEncoder_QuietDemotesToDebug(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	sm := &StreamMux[struct{}]{}
	sm.QuietOnMissingEncoder.Store(true)

	h := &AutoBitRateHandler[struct{}]{StreamMux: sm}
	h.logMissingEncoder(ctx)

	levels := levelsForMissingEncoder(hook.snapshot())
	require.Equal(t, []logger.Level{logger.LevelDebug}, levels,
		"QuietOnMissingEncoder=true must demote to Debug; got %v", levels)
}
