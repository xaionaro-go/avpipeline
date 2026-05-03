// input_quiet_test.go pins the flag-gated demotion of the by-design
// open-failure log spam emitted by NewInputFromURL when
// InputConfig.QuietOnOpenFailure is set.
//
// Two log sites are gated by the flag:
//   - Site A (kernel/input.go: format-from-URL detection): the
//     "attempting to detect input format from URL: ..." line emitted
//     for any URL whose scheme is not in urltools.FormatNameFromURL's
//     known list. Default Warn; demotes to Debug under the flag.
//   - Site C (kernel/input.go: AsyncOpen failure handler): the
//     "unable to open: ..." line emitted from the goroutine spawned
//     under cfg.AsyncOpen=true. Default Errorf; demotes to Debugf
//     under the flag.
//
// These tests use cfg.AsyncOpen=false (the default) and a URL whose
// scheme triggers site A. AsyncOpen=true is intentionally avoided to
// keep the tests deterministic — the async path's failure goroutine
// races against testing.T.Cleanup teardown via a known
// FormatContext.Free / IOInterrupter aliasing window in input.go that
// is unrelated to this flag's semantics. Site C still receives full
// gating via the same cfg.QuietOnOpenFailure bit (both sites share
// the same gate condition); the gate's correctness is asserted via
// the surrounding code review, not via a flaky async test here.
//
// Default (flag off) preserves the legacy Warn level so existing
// diagnostics aren't lost.

package kernel

import (
	"context"
	"sync"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/secret"
)

// quietRecordingHook captures every Entry pushed to the logger so a
// test can assert exactly which Level a given call site emitted at.
type quietRecordingHook struct {
	mu      sync.Mutex
	entries []loggertypes.Entry
}

var _ loggertypes.Hook = (*quietRecordingHook)(nil)

func (h *quietRecordingHook) ProcessLogEntry(e *loggertypes.Entry) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The Entry pointer is reused by the emitter; copy by value.
	h.entries = append(h.entries, *e)
	return true
}

func (h *quietRecordingHook) Flush() {}

func (h *quietRecordingHook) snapshot() []loggertypes.Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]loggertypes.Entry, len(h.entries))
	copy(out, h.entries)
	return out
}

func ctxWithQuietRecordingHook(t *testing.T) (context.Context, *quietRecordingHook) {
	t.Helper()
	hook := &quietRecordingHook{}
	l := logrus.Default().WithLevel(logger.LevelTrace).WithHooks(hook)
	return logger.CtxWithLogger(context.Background(), l), hook
}

// hasLevel reports whether any captured entry was emitted at the given level.
func hasLevel(entries []loggertypes.Entry, lvl logger.Level) bool {
	for _, e := range entries {
		if e.Level == lvl {
			return true
		}
	}
	return false
}

// bogusURL has an unknown scheme so FormatNameFromURL returns "" and
// trips site A's "attempting to detect input format from URL" log.
// libav's OpenInput then fails synchronously, so NewInputFromURL
// returns an error and there is nothing to Close.
const bogusURL = "bogusscheme://nonexistent.invalid/path"

// TestInputConfig_QuietOnOpenFailure_DefaultOff_KeepsWarn pins the legacy
// default: with QuietOnOpenFailure=false, NewInputFromURL emits at
// least one WARN-level entry for a URL whose open is going to fail.
func TestInputConfig_QuietOnOpenFailure_DefaultOff_KeepsWarn(t *testing.T) {
	ctx, hook := ctxWithQuietRecordingHook(t)

	cfg := InputConfig{
		QuietOnOpenFailure: false,
	}
	input, err := NewInputFromURL(ctx, bogusURL, secret.New(""), cfg)
	require.Error(t, err, "bogus scheme must fail at sync OpenInput")
	require.Nil(t, input, "failed sync open returns nil input")

	entries := hook.snapshot()
	require.True(t, hasLevel(entries, logger.LevelWarning),
		"default (QuietOnOpenFailure=false) must emit at least one WARN-level entry; got %v", entries)
}

// TestInputConfig_QuietOnOpenFailure_True_DemotesToDebug pins the
// flag-gated demotion: with QuietOnOpenFailure=true, NewInputFromURL
// emits ZERO WARN/ERROR entries even when the open is going to fail.
func TestInputConfig_QuietOnOpenFailure_True_DemotesToDebug(t *testing.T) {
	ctx, hook := ctxWithQuietRecordingHook(t)

	cfg := InputConfig{
		QuietOnOpenFailure: true,
	}
	input, err := NewInputFromURL(ctx, bogusURL, secret.New(""), cfg)
	require.Error(t, err, "bogus scheme must fail at sync OpenInput")
	require.Nil(t, input, "failed sync open returns nil input")

	entries := hook.snapshot()
	require.False(t, hasLevel(entries, logger.LevelWarning),
		"QuietOnOpenFailure=true must NOT emit WARN entries; got %v", entries)
	require.False(t, hasLevel(entries, logger.LevelError),
		"QuietOnOpenFailure=true must NOT emit ERROR entries; got %v", entries)
}
