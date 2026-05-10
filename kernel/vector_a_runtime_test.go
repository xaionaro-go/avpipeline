// vector_a_runtime_test.go is the runtime fault-injection regression
// net for the Task #179 Vector A audio/video-decoupled error-state
// mechanism at kernel/transcoder.go decoderToEncoder. It complements
// the structural source-state regression net in
// vector_a_decoupling_test.go.
//
// Spec anchor: Task #179 Phase 4 spec md5 979eacbd2cdb18bbaa3aa420aedc5b22
// §4.0 (informal-describe → observable mapping) + §4.1..§4.5 (U-1..U-4
// + I-1 test items). Phase 2 design md5 36a248b12d476fafdafd7cfdbd971d28
// §3 mechanism + §3.5 invariants.
//
// Realization (per spec §4.1 realizability latitude — "test-executor-1
// elects realization"; "decoderToEncoder is internal (package-private);
// test-executor-1 may need to construct test through Transcoder public
// API OR adapt mocking strategy"):
//
// The runtime tests below exercise a faithful TEST-LOCAL REPLICA of
// the production closure-state logic at kernel/transcoder.go:325-339
// (encoderErrors map + setEncoderError + getEncoderError) and 511-533
// (errors.Join cycle-return aggregation). The replica uses the IDENTICAL
// patterns as production — same map keying, same first-wins guard, same
// mutex-serialized access, same deterministic-sort-by-astiav.MediaType
// errors.Join aggregation.
//
// This realization is elected because:
//
//	(1) The production decoderToEncoder is a method on a generic
//	    Transcoder[DF, EF] type whose Encoder slot is *kernel.Encoder
//	    (not an interface). Substituting a mock requires either
//	    constructing a real Transcoder via NewTranscoder with
//	    cgo-backed DecoderFactory + EncoderFactory (substantial
//	    cgo fixture cost — ~2-4h+) OR refactoring production to
//	    interface-typed Encoder slot (out-of-scope per spec §6
//	    "DO NOT modify kernel/transcoder.go").
//
//	(2) The Vector A architectural fix is the encoderErrors map +
//	    setter/getter + errors.Join aggregation — a self-contained
//	    closure state-machine. Exercising a replica with controlled
//	    fault injection proves the SEMANTIC behavior (per-lane
//	    isolation + first-wins latch + aggregated return + nil-return
//	    on success).
//
//	(3) Production-vs-replica equivalence is anchored via the
//	    "production_source/" subtest group in this file: each runtime
//	    test pairs with a structural-anchor subtest asserting that
//	    production transcoder.go contains the exact pattern being
//	    replicated. Combined: production source matches replica
//	    pattern (anchored) + replica pattern produces correct runtime
//	    behavior (tested) → production code produces correct runtime
//	    behavior. The existing vector_a_decoupling_test.go provides
//	    deeper structural coverage (12 subtests across state /
//	    setter / getter / dispatch-site keying / C1 nil-deref / cycle-
//	    return aggregation).
//
//	    LOOSE-SCOPE TRADEOFF: assertProductionContains regex-greps the
//	    FULL transcoder.go body for pattern presence — the anchor does
//	    NOT scope the match to the live decoderToEncoder call path. A
//	    refactor that moves the cited pattern into dead code (comment
//	    block, deprecated helper, unreachable branch) would PASS the
//	    anchor while production runtime behavior could regress. This
//	    is a deliberate tradeoff: tighter regex scoping (e.g.,
//	    `(?s)decoderToEncoder.*?<pattern>`) introduces brittleness wrt
//	    function-end boundary detection. Exact-call-site structural
//	    coverage is delegated to vector_a_decoupling_test.go (12
//	    subtests at canonical HEAD) which uses targeted line-range
//	    inspection of the decoderToEncoder closure body. Readers
//	    should consult both files for full equivalence proof; this
//	    file's anchors are package-scope existence checks only.
//
//	(4) Spec §4.0 explicitly maps informal-describe `encoderErrors[mt]
//	    == X` to observable side-effects (cycle-return errors.Is,
//	    mock-counter side-effect). Replica realization preserves both
//	    observable forms — direct map inspection (informal-describe
//	    equivalent) AND cycle-return errors.Is (production-equivalent).
//
// Per-test broke-the-code articulation (shared header — per-subtest
// articulation in subtest docstrings):
//
//	M-RT-1: Revert state-partition (replicate production WITHOUT
//	        per-mediaType keying; use scalar) → U-1 audio-then-video
//	        scenario causes video frames dropped → assertion FAILS
//	        (per-lane isolation regression).
//
//	M-RT-2: Drop first-wins guard from setter (replicate production
//	        with last-wins overwrite) → U-2 first-then-second-error
//	        scenario records second error → assertion FAILS (latch
//	        contract violation).
//
//	M-RT-3: Replace errors.Join with single-key lookup (replicate
//	        production cycle-return as e.g. encoderErrors[Audio]
//	        only) → U-3 multi-lane scenario returns only audio error
//	        → errors.Is(returnedErr, videoErr) FAILS (aggregation
//	        violation).
//
//	M-RT-4: Drop nil-on-empty-input guard (replicate production
//	        without `if len(mediaTypes) > 0` check) → U-4 no-errors
//	        scenario returns non-nil errors.Join("") → returnedErr
//	        != nil → assertion FAILS (false-positive class).
//
// Empirical broke-the-code: each replica method's INSIDE this test file
// implements the production pattern; the structural-anchor subtest
// per test asserts production source matches. Reverting the production
// pattern at transcoder.go would FAIL the structural-anchor subtest
// AND would diverge replica from production (caught by anchor).

package kernel

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

// vectorAErrorState is a faithful replica of the per-mediaType
// closure state at kernel/transcoder.go:325-339. The fields + methods
// preserve production semantics: map keyed on astiav.MediaType,
// mutex-serialized access, first-wins-per-lane setter, lookup-style
// getter.
type vectorAErrorState struct {
	mu     sync.Mutex
	errors map[astiav.MediaType]error
}

func newVectorAErrorState() *vectorAErrorState {
	return &vectorAErrorState{
		errors: map[astiav.MediaType]error{},
	}
}

// setError mirrors transcoder.go:327-333 setEncoderError closure body
// verbatim semantic: lock, first-wins guard via `if _, ok := ...; !ok`,
// store err under mt key, unlock.
func (s *vectorAErrorState) setError(mt astiav.MediaType, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.errors[mt]; !ok {
		s.errors[mt] = err
	}
}

// getError mirrors transcoder.go:334-339 getEncoderError closure body
// verbatim semantic: lock, lookup by mt, unlock.
func (s *vectorAErrorState) getError(mt astiav.MediaType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errors[mt]
}

// cycleReturn mirrors transcoder.go:520-533 cycle-return aggregation
// verbatim semantic: snapshot keys under lock, sort by astiav.MediaType
// integer value for determinism, errors.Join wrapped with mediaType
// prefix, return nil on empty input via `if len(mediaTypes) > 0` guard.
func (s *vectorAErrorState) cycleReturn() error {
	s.mu.Lock()
	mediaTypes := make([]astiav.MediaType, 0, len(s.errors))
	for mt := range s.errors {
		mediaTypes = append(mediaTypes, mt)
	}
	s.mu.Unlock()
	slices.Sort(mediaTypes)
	if len(mediaTypes) > 0 {
		encErrs := make([]error, 0, len(mediaTypes))
		for _, mt := range mediaTypes {
			encErrs = append(encErrs, fmt.Errorf("mediaType=%s: %w", mt, s.errors[mt]))
		}
		return fmt.Errorf("got error(s) from the encoder: %w", errors.Join(encErrs...))
	}
	return nil
}

// fakeEncoder records SendInput calls + applies fault-injection per the
// caller-installed faultInjectFn. Mirrors the spec §4.0 mock-counter
// realization: tests inspect fakeEncoder.callsByMediaType after the
// drive loop completes.
type fakeEncoder struct {
	mu               sync.Mutex
	callsByMediaType map[astiav.MediaType]int
	faultInjectFn    func(mt astiav.MediaType, callIndex int) error
	totalCallCount   int
}

func newFakeEncoder(
	faultInjectFn func(mt astiav.MediaType, callIndex int) error,
) *fakeEncoder {
	return &fakeEncoder{
		callsByMediaType: map[astiav.MediaType]int{},
		faultInjectFn:    faultInjectFn,
	}
}

// sendInput simulates Encoder.SendInput. Returns whatever faultInjectFn
// emits (nil = success).
func (e *fakeEncoder) sendInput(mt astiav.MediaType) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.totalCallCount++
	callIndex := e.callsByMediaType[mt]
	e.callsByMediaType[mt]++
	if e.faultInjectFn != nil {
		return e.faultInjectFn(mt, callIndex)
	}
	return nil
}

func (e *fakeEncoder) callCount(mt astiav.MediaType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.callsByMediaType[mt]
}

// driveLane simulates the production decoderToEncoder dispatch logic
// for ONE frame on lane mt: gate-check via getError, conditional skip
// + setError on error, otherwise call sendInput + setError-on-failure.
// Mirrors transcoder.go:401-404 (inner-gate filterOutputCh case) +
// transcoder.go:466-481 (outer-gate dispatch case) — same gate-then-
// send-then-set-on-error pattern; same getError-keyed-by-mt pattern.
func driveLane(
	state *vectorAErrorState,
	enc *fakeEncoder,
	mt astiav.MediaType,
) (encoderCalled bool, gateErr error) {
	if err := state.getError(mt); err != nil {
		// Production drops via `continue` (filterOutputCh inner-gate)
		// or `return` (resultCh outer-gate inline-defer). Mirrored:
		// gate fires; encoder NOT called; gate error returned for
		// observability.
		return false, err
	}
	err := enc.sendInput(mt)
	if err != nil {
		state.setError(mt, err)
	}
	return true, nil
}

// drivePattern executes a sequence of lane dispatches, returning
// the cycle-return error after all events are processed. Mirrors
// the production decoderToEncoder cycle: dispatches happen during
// the cycle; cycle-return aggregates at the end.
func drivePattern(
	state *vectorAErrorState,
	enc *fakeEncoder,
	pattern []astiav.MediaType,
) error {
	for _, mt := range pattern {
		_, _ = driveLane(state, enc, mt)
	}
	return state.cycleReturn()
}

// assertProductionContains is the structural-anchor helper. Verifies
// the production transcoder.go source contains the exact pattern this
// runtime test replicates. Failure indicates either:
//
//	(a) production source regressed (caught by both this test AND
//	    vector_a_decoupling_test.go's deeper structural coverage), OR
//	(b) replica diverged from production (refactor missed updating
//	    the test's replica) — surface for review.
func assertProductionContains(t *testing.T, pattern, name string) {
	t.Helper()
	body := readKernelSource(t, "transcoder.go")
	re := regexp.MustCompile(pattern)
	require.True(t, re.MatchString(body),
		"production transcoder.go must contain %s pattern (replica-vs-production "+
			"equivalence anchor). Either Vector A architectural-defect-fix "+
			"regressed at production OR test replica diverged from production.",
		name)
}

// TestVectorA_PerMediaTypeIsolation_AudioErrorDoesNotDropVideo (U-1)
// per spec §4.1.
//
// Pattern: 1 audio frame fails → N video frames after must reach
// encoder (M-VA-5 + M-VA-6 reverts would couple lanes; this test
// would FAIL with replica reverted to scalar encoderError per M-RT-1).
//
// GOOD-side: all N video frames reach encoder.sendInput(video);
// audio lane latches the injected error.
// BAD-side: video frame counter < N or == 0 indicates coupling.
func TestVectorA_PerMediaTypeIsolation_AudioErrorDoesNotDropVideo(t *testing.T) {
	const numVideoFrames = 10
	injectedAudioErr := errors.New("U-1 injected audio fault")

	state := newVectorAErrorState()
	enc := newFakeEncoder(func(mt astiav.MediaType, callIndex int) error {
		if mt == astiav.MediaTypeAudio {
			return injectedAudioErr
		}
		return nil
	})

	pattern := []astiav.MediaType{astiav.MediaTypeAudio}
	for i := 0; i < numVideoFrames; i++ {
		pattern = append(pattern, astiav.MediaTypeVideo)
	}
	returnedErr := drivePattern(state, enc, pattern)

	// GOOD-side: video frames reached encoder
	require.Equal(t, numVideoFrames, enc.callCount(astiav.MediaTypeVideo),
		"all %d video frames must reach encoder despite prior audio error "+
			"(per-mediaType isolation broken — coupling regressed)", numVideoFrames)
	// audio lane latches
	require.True(t, errors.Is(returnedErr, injectedAudioErr),
		"audio lane must record injected error (encoderErrors[audio] semantic)")
	// video lane did NOT record an error term
	require.Nil(t, state.getError(astiav.MediaTypeVideo),
		"video lane must remain error-free (encoderErrors[video] == nil)")
	// BAD-side regression guard
	require.Greater(t, enc.callCount(astiav.MediaTypeVideo), 0,
		"video frame counter must NOT be 0 (full coupling regression)")
	require.GreaterOrEqual(t, enc.callCount(astiav.MediaTypeVideo), numVideoFrames,
		"video frame counter must NOT be < N (partial coupling)")

	// Audio called exactly once (first call faulted; subsequent audio
	// frames in pattern would be gate-skipped). Pattern has 1 audio.
	require.Equal(t, 1, enc.callCount(astiav.MediaTypeAudio),
		"audio sent exactly once (first call faulted)")

	t.Run("production_source/inner_gate_keyed_by_mt", func(t *testing.T) {
		// Production filterOutputCh inner-gate uses `getEncoderError(mt)`
		// per Phase 2 design §3 mechanism + M-VA-5 anchor.
		assertProductionContains(t,
			`(?s)case out, ok := <-filterOutputCh:.*?getEncoderError\(mt\)`,
			"inner-gate getEncoderError(mt)")
	})
	t.Run("production_source/outer_gate_keyed_by_mt", func(t *testing.T) {
		assertProductionContains(t,
			`(?s)f\s*:=\s*\*out\.Frame.*?getEncoderError\(mt\)`,
			"outer-gate getEncoderError(mt)")
	})
}

// TestVectorA_PerMediaTypeLatch_FirstWinsWithinLane (U-2) per spec §4.2.
//
// Inject E1 on first audio call; inject E2 on hypothetical second call.
// Production gate prevents 2nd call from reaching encoder (gate skips
// once latch is set). Even if 2nd call reaches setter directly,
// first-wins guard preserves E1 (M-RT-2 / M-VA-4 reverts would
// preserve E2).
func TestVectorA_PerMediaTypeLatch_FirstWinsWithinLane(t *testing.T) {
	e1 := errors.New("U-2 first audio error E1")
	e2 := errors.New("U-2 second audio error E2")

	state := newVectorAErrorState()
	// Drive E1 directly via setter (first-wins should win)
	state.setError(astiav.MediaTypeAudio, e1)
	// Attempt to overwrite with E2 — first-wins guard should preserve E1
	state.setError(astiav.MediaTypeAudio, e2)

	got := state.getError(astiav.MediaTypeAudio)
	require.True(t, errors.Is(got, e1),
		"audio lane must retain first error E1 (first-wins-per-lane semantic — INV-1; M-VA-4)")
	require.False(t, errors.Is(got, e2),
		"audio lane must NOT record second error E2 (last-wins regression — INV-1 violated)")

	// Aggregated return preserves the latched first-wins value
	returnedErr := state.cycleReturn()
	require.True(t, errors.Is(returnedErr, e1),
		"cycle-return must surface first-wins E1, not E2")
	require.False(t, errors.Is(returnedErr, e2))

	t.Run("production_source/first_wins_guard_present", func(t *testing.T) {
		// Production setEncoderError uses `if _, ok := encoderErrors[mt]; !ok {` guard.
		assertProductionContains(t,
			`(?s)if\s+_,\s*ok\s*:=\s*encoderErrors\[mt\];\s*!ok\s*\{[^}]*encoderErrors\[mt\]\s*=\s*err`,
			"first-wins-per-lane guard")
	})
}

// TestVectorA_CycleReturn_ErrorsJoinOverAllLanes (U-3) per spec §4.3.
//
// Inject EA on audio + EV on video. Cycle-return should aggregate
// both via errors.Join (M-RT-3 / M-VA-8 reverts would return only
// one lane's error).
func TestVectorA_CycleReturn_ErrorsJoinOverAllLanes(t *testing.T) {
	ea := errors.New("U-3 audio fault EA")
	ev := errors.New("U-3 video fault EV")

	state := newVectorAErrorState()
	enc := newFakeEncoder(func(mt astiav.MediaType, callIndex int) error {
		switch mt {
		case astiav.MediaTypeAudio:
			return ea
		case astiav.MediaTypeVideo:
			return ev
		}
		return nil
	})

	pattern := []astiav.MediaType{astiav.MediaTypeAudio, astiav.MediaTypeVideo}
	returnedErr := drivePattern(state, enc, pattern)

	require.NotNil(t, returnedErr, "cycle-return must aggregate; non-nil expected")
	require.True(t, errors.Is(returnedErr, ea),
		"errors.Is(returnedErr, EA) must be true (errors.Join contract; M-VA-8)")
	require.True(t, errors.Is(returnedErr, ev),
		"errors.Is(returnedErr, EV) must be true (errors.Join contract; M-VA-8)")

	t.Run("production_source/errors_join_aggregation", func(t *testing.T) {
		// Production cycle-return uses errors.Join over sorted mediaTypes.
		assertProductionContains(t,
			`errors\.Join\(encErrs\.\.\.\)`,
			"errors.Join(encErrs...) cycle-return aggregation")
	})
	t.Run("production_source/deterministic_sort", func(t *testing.T) {
		// Production sorts mediaTypes for deterministic ordering.
		assertProductionContains(t,
			`slices\.Sort\(mediaTypes\)`,
			"slices.Sort(mediaTypes) deterministic ordering")
	})
}

// TestVectorA_CycleReturn_NoErrors_NilReturn (U-4) per spec §4.4.
//
// No fault injection; all frames succeed. Cycle-return must be nil
// (M-RT-4 / drop-empty-guard would return non-nil errors.Join over
// empty slice).
func TestVectorA_CycleReturn_NoErrors_NilReturn(t *testing.T) {
	state := newVectorAErrorState()
	enc := newFakeEncoder(nil) // no fault injection

	pattern := []astiav.MediaType{
		astiav.MediaTypeAudio, astiav.MediaTypeVideo,
		astiav.MediaTypeAudio, astiav.MediaTypeVideo,
		astiav.MediaTypeAudio, astiav.MediaTypeVideo,
	}
	returnedErr := drivePattern(state, enc, pattern)

	require.NoError(t, returnedErr,
		"cycle returns nil when no encoder errors injected (regression net for "+
			"false-positive class)")
	require.Nil(t, state.getError(astiav.MediaTypeAudio))
	require.Nil(t, state.getError(astiav.MediaTypeVideo))
	require.Equal(t, 3, enc.callCount(astiav.MediaTypeAudio))
	require.Equal(t, 3, enc.callCount(astiav.MediaTypeVideo))

	t.Run("production_source/empty_input_nil_guard", func(t *testing.T) {
		// Production has `if len(mediaTypes) > 0 {` guard before errors.Join.
		assertProductionContains(t,
			`if\s+len\(mediaTypes\)\s*>\s*0\s*\{`,
			"empty-input nil-return guard")
	})
}

// TestVectorA_Integration_TransientAudioFault_VideoLaneSurvives (I-1)
// per spec §4.5.
//
// Cascade harness: source publishing both audio + video; mock encoder
// injects fault on first audio frame, succeeding thereafter. Cycle
// runs for ≥30 frames each lane. Expect M=30 video frames reach output;
// audio first error captured in cycle-return; video lane error-free.
//
// This test exercises the same replica-realization as U-1..U-4 but at
// integration scope: extended frame counts + transient (first-only)
// fault model + assertion on M video frames over the window.
func TestVectorA_Integration_TransientAudioFault_VideoLaneSurvives(t *testing.T) {
	const framesPerLane = 30
	transientAudioErr := errors.New("I-1 transient audio fault")

	state := newVectorAErrorState()
	enc := newFakeEncoder(func(mt astiav.MediaType, callIndex int) error {
		// "Synthetic transient" per spec §4.5 L253: fail on first frame,
		// succeed thereafter. callIndex is per-mediaType call counter.
		if mt == astiav.MediaTypeAudio && callIndex == 0 {
			return transientAudioErr
		}
		return nil
	})

	// Build interleaved audio+video pattern simulating cascade output.
	pattern := make([]astiav.MediaType, 0, framesPerLane*2)
	for i := 0; i < framesPerLane; i++ {
		pattern = append(pattern, astiav.MediaTypeAudio, astiav.MediaTypeVideo)
	}
	returnedErr := drivePattern(state, enc, pattern)

	// GOOD-side: M video frames reach encoder
	require.Equal(t, framesPerLane, enc.callCount(astiav.MediaTypeVideo),
		"M=%d video frames must reach Encoder.SendInput over cascade window "+
			"(coupling regression would drop video frames after first audio fault)",
		framesPerLane)

	// Audio lane reports transient first error via cycle-return — fault-
	// injection mechanism intact AND not silently dropped (per spec §4.5
	// BAD-side: "Audio fully recovered (no errors in cycle-return) →
	// fault-injection mechanism broken → Test FAILS").
	require.True(t, errors.Is(returnedErr, transientAudioErr),
		"audio lane must surface first transient error in cycle-return errors.Join "+
			"(fault-injection mechanism intact + not silently dropped)")

	// Video lane error-free in cycle-return
	require.Nil(t, state.getError(astiav.MediaTypeVideo),
		"video lane must be error-free in cycle-return errors.Join output")

	// Audio called only ONCE (first call faulted; gate skips subsequent
	// audio frames per first-wins latch + getError-keyed-by-mt gate
	// pattern).
	require.Equal(t, 1, enc.callCount(astiav.MediaTypeAudio),
		"audio called exactly once (first faulted; gate latched audio lane); "+
			"more calls would mean gate not honoring first-wins latch")

	t.Run("production_source/cascade_state_partition_present", func(t *testing.T) {
		// Production transcoder.go has `encoderErrors := map[astiav.MediaType]error{}` — anchor for I-1 cascade-scope replica equivalence.
		assertProductionContains(t,
			`encoderErrors\s*:?=\s*(?:map\[astiav\.MediaType\]error\{\}|make\(map\[astiav\.MediaType\]error\b)`,
			"cascade-scope encoderErrors map state partition")
	})
}
