// quiet_on_open_failure_test.go pins the flag-gated demotion of
// by-design open-failure log spam:
//
//   - input_chain.go:    "input N error: ..." Errorf -> Debugf when
//     QuietOnOpenFailure is enabled. Covers BOTH:
//       (a) factory.HasResources()=false (empty fallback slot waiting
//           for a resource), and
//       (b) factory.HasResources()=true but NewInput's underlying
//           OpenInput failed (e.g. rtmp upstream not yet publishing).
//           The operator opted in via -quiet_on_open_failure (legacy
//           alias -quiet_empty_priority) and accepted the tradeoff
//           that a genuinely typo'd URL also stays at Debug.
//   - input_with_fallback.go: "onInputChainError: unable to switch to
//     fallback N: another switch is in progress (...)" Errorf -> Debugf
//     when QuietOnOpenFailure is enabled AND the SetValue error is
//     ErrSwitchInProgress (the by-design startup-walk contention
//     across consecutive empty slots).
//
// Default (flag off) preserves the legacy ERRO levels so existing
// diagnostics aren't lost. Tests exercise both arms.

package inputwithfallback

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

// --- recordingHook captures every Entry pushed to the logger so a
// test can assert exactly which Level a given call site emitted at.

type recordingHook struct {
	mu      sync.Mutex
	entries []loggertypes.Entry
}

var _ loggertypes.Hook = (*recordingHook)(nil)

func (h *recordingHook) ProcessLogEntry(e *loggertypes.Entry) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The Entry pointer is reused by the emitter; copy by value.
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

// ctxWithRecordingHook builds a fresh context wired to a logrus logger
// configured at TraceLevel so neither Debug nor Error entries are
// filtered before reaching the hook.
func ctxWithRecordingHook(t *testing.T) (context.Context, *recordingHook) {
	t.Helper()
	hook := &recordingHook{}
	l := logrus.Default().WithLevel(logger.LevelTrace).WithHooks(hook)
	return logger.CtxWithLogger(context.Background(), l), hook
}

// findLevelsForMessage returns every Entry whose Message contains the
// given substring. Substring rather than exact-match because go-belt
// formats with leading "INFO[NNN]…"-style prefixes only at the
// emitter; the Hook receives the raw Message.
func findLevelsForMessage(entries []loggertypes.Entry, sub string) []logger.Level {
	var out []logger.Level
	for _, e := range entries {
		if containsString(e.Message, sub) {
			out = append(out, e.Level)
		}
	}
	return out
}

func containsString(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- Site A: input_chain.go's Retryable OnError ---

// TestInputChain_NoResourcesConfigured_DefaultLogsAtError pins the
// legacy default: with QuietOnOpenFailure=false (the default), the
// Retryable OnError closure logs "input N error: ..." at Errorf even
// when the factory reports HasResources=false.
func TestInputChain_NoResourcesConfigured_DefaultLogsAtError(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	factory := &mockInputFactoryWithAvailability{
		mockInputFactory: mockInputFactory{name: "empty-prio-default"},
		hasResources:     false,
	}
	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()
	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx, 0, factory,
		filterSwitch.Output(0), syncSwitch.Output(0),
		false, // QuietOnOpenFailure OFF (legacy default)
		0,     // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	// Drive the OnError path with the same kind of error NewInput would
	// return for an empty priority slot.
	emptyErr := fmt.Errorf("no input resources configured for priority %d", 0)
	_ = chain.Input.Processor.Kernel.OnError(ctx, nil, emptyErr)

	levels := findLevelsForMessage(hook.snapshot(), "input 0 error:")
	require.NotEmpty(t, levels, "OnError must emit the 'input N error' line")
	require.Contains(t, levels, logger.LevelError,
		"default (QuietOnOpenFailure=false) must keep the legacy Errorf "+
			"so existing diagnostics are not lost; got levels %v", levels)
}

// TestInputChain_NoResourcesConfigured_QuietLogsAtDebug pins the
// flag-gated demotion: with QuietOnOpenFailure=true AND
// factory.HasResources()=false, the "input N error" message demotes to
// Debug — at this site the message text is identical to the legacy
// Errorf (the gate covers all OnError invocations under the flag),
// so we assert ZERO Errorf entries instead of a tagged Debug line.
func TestInputChain_NoResourcesConfigured_QuietLogsAtDebug(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	factory := &mockInputFactoryWithAvailability{
		mockInputFactory: mockInputFactory{name: "empty-prio-quiet"},
		hasResources:     false,
	}
	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()
	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx, 0, factory,
		filterSwitch.Output(0), syncSwitch.Output(0),
		true, // QuietOnOpenFailure ON
		0,    // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	emptyErr := fmt.Errorf("no input resources configured for priority %d", 0)
	_ = chain.Input.Processor.Kernel.OnError(ctx, nil, emptyErr)

	entries := hook.snapshot()
	levels := findLevelsForMessage(entries, "input 0 error:")
	require.NotEmpty(t, levels,
		"OnError must emit the 'input N error' line at SOME level")
	for _, lvl := range levels {
		require.Equal(t, logger.LevelDebug, lvl,
			"empty-priority message must be Debug when flag is set; got %v at %v",
			entries, lvl)
	}
}

// TestInputChain_OpenFailureOccupiedPriority_QuietDemotesToDebug:
// when the factory reports HasResources=true (priority slot has a
// configured URL/resource) but NewInput's underlying OpenInput fails,
// the retry-loop OnError invocation must demote to Debug under
// QuietOnOpenFailure=true. Covers the rtmp-upstream-not-publishing
// case.
func TestInputChain_OpenFailureOccupiedPriority_QuietDemotesToDebug(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	factory := &mockInputFactoryWithAvailability{
		mockInputFactory: mockInputFactory{name: "occupied-prio-quiet"},
		hasResources:     true, // chain HAS resources; underlying open failed
	}
	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()
	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx, 0, factory,
		filterSwitch.Output(0), syncSwitch.Output(0),
		true, // QuietOnOpenFailure ON
		0,    // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	// Mimic the kind of error NewInputFromURL returns when the upstream
	// rtmp publisher is not yet connected: a wrapped libav OpenInput
	// failure. The exact substring is irrelevant — the gate looks at
	// the flag, not at the error class.
	openErr := fmt.Errorf("unable to open input by URL %q: I/O error", "rtmp://...")
	_ = chain.Input.Processor.Kernel.OnError(ctx, nil, openErr)

	entries := hook.snapshot()
	levels := findLevelsForMessage(entries, "input 0 error:")
	require.NotEmpty(t, levels,
		"OnError must emit the 'input N error' line at SOME level")
	for _, lvl := range levels {
		require.Equal(t, logger.LevelDebug, lvl,
			"open-failure on occupied priority must be Debug when flag is set")
	}
	require.False(t, hasMessageAtLevel(entries, "input 0 error:", logger.LevelError),
		"flag-gated path must not emit Errorf even when HasResources=true")
}

// TestInputChain_OpenFailureOccupiedPriority_DefaultKeepsErrorf pins
// the legacy default for the same scenario as above: with
// QuietOnOpenFailure=false, an open-failure on an occupied priority
// keeps Errorf so existing diagnostics are not lost.
func TestInputChain_OpenFailureOccupiedPriority_DefaultKeepsErrorf(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)

	factory := &mockInputFactoryWithAvailability{
		mockInputFactory: mockInputFactory{name: "occupied-prio-default"},
		hasResources:     true,
	}
	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()
	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx, 0, factory,
		filterSwitch.Output(0), syncSwitch.Output(0),
		false, // QuietOnOpenFailure OFF (legacy default)
		0,     // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	openErr := fmt.Errorf("connection refused")
	_ = chain.Input.Processor.Kernel.OnError(ctx, nil, openErr)

	levels := findLevelsForMessage(hook.snapshot(), "input 0 error: connection refused")
	require.NotEmpty(t, levels, "must emit 'input N error' for occupied priority")
	require.Contains(t, levels, logger.LevelError,
		"default (flag off) must keep Errorf so existing diagnostics are not lost")
}

// hasMessageAtLevel reports whether any captured entry whose Message
// contains the given substring was emitted at the given level.
func hasMessageAtLevel(entries []loggertypes.Entry, sub string, lvl logger.Level) bool {
	for _, e := range entries {
		if containsString(e.Message, sub) && e.Level == lvl {
			return true
		}
	}
	return false
}

// --- Site C: ErrSwitchInProgress sentinel + onInputChainError demote ---

// TestErrSwitchInProgress_IsSentinel asserts that the error returned
// from the OnSwitchRequest gate while selector switch-progress work is
// in flight is the
// ErrSwitchInProgress type, so callers can distinguish it via
// errors.Is.
func TestErrSwitchInProgress_IsSentinel(t *testing.T) {
	err := error(ErrSwitchInProgress{ProcN: 1, To: 2})
	require.True(t, errors.Is(err, ErrSwitchInProgress{}),
		"errors.Is must match ErrSwitchInProgress sentinel")
	require.Contains(t, err.Error(), "another switch is in progress",
		"Error() must preserve the legacy message text")
	wrapped := fmt.Errorf("outer: %w", err)
	require.True(t, errors.Is(wrapped, ErrSwitchInProgress{}),
		"errors.Is must traverse fmt.Errorf wrapping")
}

// --- Site C: onInputChainError demote on ErrSwitchInProgress ---

// TestInputWithFallback_OnInputChainError_QuietDemotesSwitchInProgress
// pins the flag-gated demotion of the
// "unable to switch to fallback N: another switch is in progress"
// cascade: when QuietOnOpenFailure is true and SetValue fails with
// ErrSwitchInProgress, the message demotes to Debug. Default behavior
// is exercised by
// TestInputWithFallback_OnInputChainError_DefaultLogsSwitchInProgressAtError.
func TestInputWithFallback_OnInputChainError_QuietDemotesSwitchInProgress(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Two empty fallback chains past the active one. The active chain
	// (id 0) must report HasResources=true so the walk reaches the
	// SetValue call (an active chain is needed for the
	// "current == int(id)" guard to pass). Pre-occupy the selector
	// switch-progress gate so SetValue's OnSwitchRequest gate returns
	// ErrSwitchInProgress.
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for i := 0; i < 3; i++ {
		f := &mockInputFactoryWithAvailability{
			mockInputFactory: mockInputFactory{name: fmt.Sprintf("factory-%d", i)},
			hasResources:     i == 0 || i == 2,
		}
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx, iFactories,
		OptionQuietOnOpenFailure(true),
	)
	require.NoError(t, err)
	defer func() {
		cancel()
		_ = iwf.Close(context.Background())
	}()

	// Park the selector switch-progress gate above zero so
	// OnSwitchRequest trips and SetValue returns ErrSwitchInProgress,
	// exercising the demote path at input_with_fallback.go's
	// onInputChainError site.
	work, err := iwf.switchGate.StartRequest(id.MemberID(99))
	require.NoError(t, err)
	defer work.Release()

	res := iwf.onInputChainError(ctx, iwf.InputChains[0], errors.New("primary failed"))
	require.NoError(t, res)

	entries := hook.snapshot()
	debugLevels := findLevelsForMessage(entries, "superseded by in-flight switch")
	require.NotEmpty(t, debugLevels,
		"flag-gated path must emit a 'superseded by in-flight switch' Debug line")
	for _, lvl := range debugLevels {
		require.Equal(t, logger.LevelDebug, lvl,
			"in-progress contention message must be Debug when flag is set")
	}
	errorLevels := findLevelsForMessage(entries, "unable to switch to fallback")
	for _, lvl := range errorLevels {
		require.NotEqual(t, logger.LevelError, lvl,
			"flag-gated path must not emit Errorf for the in-progress contention case")
	}
}

// TestInputWithFallback_OnInputChainError_DefaultLogsSwitchInProgressAtError
// pins legacy behavior: with the flag off, the
// "unable to switch to fallback N" message keeps Errorf.
func TestInputWithFallback_OnInputChainError_DefaultLogsSwitchInProgressAtError(t *testing.T) {
	ctx, hook := ctxWithRecordingHook(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for i := 0; i < 3; i++ {
		f := &mockInputFactoryWithAvailability{
			mockInputFactory: mockInputFactory{name: fmt.Sprintf("factory-%d", i)},
			hasResources:     i == 0 || i == 2,
		}
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	defer func() {
		cancel()
		_ = iwf.Close(context.Background())
	}()

	work, err := iwf.switchGate.StartRequest(id.MemberID(99))
	require.NoError(t, err)
	defer work.Release()

	res := iwf.onInputChainError(ctx, iwf.InputChains[0], errors.New("primary failed"))
	require.NoError(t, res)

	levels := findLevelsForMessage(hook.snapshot(), "unable to switch to fallback")
	require.NotEmpty(t, levels, "must emit the 'unable to switch to fallback' line")
	require.Contains(t, levels, logger.LevelError,
		"default (flag off) must keep Errorf so genuine switch contention is visible")
}
