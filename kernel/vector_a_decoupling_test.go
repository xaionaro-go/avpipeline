// vector_a_decoupling_test.go is the source-state regression net for
// the Vector A audio/video-decoupled error-state mechanism at
// kernel/transcoder.go decoderToEncoder. The mechanism replaces the
// shared `var encoderError error` (single, monotonic-latch across
// mediaTypes) with a per-mediaType map keyed on astiav.MediaType, so
// an audio-lane encoder error no longer drops video frames at the
// dispatch gates.
//
// Pattern intent (per Phase 2 design §3 mechanism):
//
//	(1) State partition:
//	    encoderErrors := map[astiav.MediaType]error{}
//	    var encoderErrorsLocker sync.Mutex
//
//	(2) Per-mediaType setter (first-wins per lane):
//	    setEncoderError := func(mt astiav.MediaType, err error) {
//	        encoderErrorsLocker.Lock()
//	        defer encoderErrorsLocker.Unlock()
//	        if _, ok := encoderErrors[mt]; !ok {
//	            encoderErrors[mt] = err
//	        }
//	    }
//
//	(3) Per-mediaType getter:
//	    getEncoderError := func(mt astiav.MediaType) error {
//	        encoderErrorsLocker.Lock()
//	        defer encoderErrorsLocker.Unlock()
//	        return encoderErrors[mt]
//	    }
//
//	(4) Dispatch sites pass mediaType:
//	    inner filterOutputCh: mt := out.GetMediaType()
//	    outer dispatch: mt := f.GetMediaType() (already in scope)
//	    setEncoderError(mt, err) at all 3 call sites
//
//	(5) Cycle-return aggregation via errors.Join over a deterministic
//	    sort of map keys.
//
//	(6) C1 LOAD-BEARING nil-deref guard at inner filterOutputCh case:
//	    `if out.Frame == nil && out.Packet == nil { continue }` BEFORE
//	    `out.GetMediaType()` (mirrors outer-loop's L458-466 nil-Frame
//	    guard, adapted for OutputUnion's Frame+Packet duality —
//	    OutputUnion.Get() returns nil interface when both are nil,
//	    causing method-on-nil-interface panic at GetMediaType).
//
// Per-test broke-the-code articulation (shared header — per-subtest
// articulation in subtest docstrings):
//
//	M-VA-1: revert state-partition declaration to `var encoderError error`
//	        → state-map-declaration assertion FAILS (matches Phase 2
//	        design §3.1 + Phase 3 mechanism — the architectural fix is
//	        the state partition; reverting it preserves coupling).
//
//	M-VA-2: drop mediaType param from setEncoderError signature
//	        → setter-signature assertion FAILS (matches Phase 2 design
//	        §3.5 interface contract — mt parameter is part of the
//	        first-wins-per-lane semantic).
//
//	M-VA-3: drop mediaType param from getEncoderError signature
//	        → getter-signature assertion FAILS (gate cannot consult a
//	        lane without naming it).
//
//	M-VA-4: drop the `if _, ok := encoderErrors[mt]; !ok {` first-wins
//	        guard in setEncoderError → first-wins-per-lane assertion
//	        FAILS (last-wins replaces first-wins; INV-1 violated).
//
//	M-VA-5: change inner-loop `getEncoderError(mt)` back to
//	        `getEncoderError()` (no-arg) → inner-gate-mediatype-keyed
//	        assertion FAILS (audio error drops video frames again;
//	        coupling regression).
//
//	M-VA-6: change outer-loop `getEncoderError(mt)` back to
//	        `getEncoderError()` → outer-gate-mediatype-keyed assertion
//	        FAILS (same coupling regression at outer dispatch site).
//
//	M-VA-7: drop the `if out.Frame == nil && out.Packet == nil {
//	        continue }` guard at inner-loop → C1-nil-deref-guard
//	        assertion FAILS (panic hazard re-introduced; design §10
//	        critique #3 + fundamentals C1 LOAD-BEARING regression).
//
//	M-VA-8: change cycle-return aggregation from `errors.Join(...)` to
//	        a single `encoderErrors[someKey]` lookup → cycle-return-aggregation
//	        assertion FAILS (one mediaType's error reported, others
//	        silently dropped — INV-3 violated).
//
//	M-VA-9: rename `encoderErrors` to `encoderError` (revert to scalar
//	        name) → all symbol-bound assertions FAIL together
//	        (compound regression).
//
//	M-VA-10: swap the C1 nil-deref guard and `mt := out.GetMediaType()`
//	        ordering at the inner filterOutputCh case (place guard
//	        AFTER GetMediaType) → c1_nildref_guard_before_GetMediaType
//	        offset-comparison FAILS (guard offset ≥ GetMediaType
//	        offset; panic hazard re-introduced even though the guard
//	        substring is still present, defeating the
//	        panic-prevention purpose).
//
//	M-VA-11: revert the locker rename `encoderErrorsLocker` →
//	        `encoderErrorLocker` (singular) → state/locker_renamed
//	        Contains assertion FAILS (symbol no longer co-named with
//	        the encoderErrors map; thread-safety contract per Phase 2
//	        §3.5 broken).
//
//	M-VA-12: drop one of the 3 setEncoderError(mt, ...) call sites OR
//	        one of the 2 getEncoderError(mt) call sites →
//	        setEncoderError_call_site_count or
//	        getEncoderError_call_site_count Equal assertion FAILS
//	        (dispatch-site enumeration drifts from Phase 2 design
//	        §3.2 + §4.2 — a missing call site means a lane is
//	        unguarded or unwritten, silently re-introducing the
//	        coupling at the missing site).
//
//	M-VA-13 (negative-discrim): weaken the cycle-return aggregation
//	        regex to a Vector-A-non-specific shape (e.g. revert to
//	        R1's `(?s)wg\.Wait\(\)[^{}]*errors\.Join\(`, or drop
//	        either the `slices\.Sort\(mediaTypes\)` anchor or the
//	        `encErrs\.\.\.` argument-name discriminator) →
//	        regex_negative_discrimination subtest FAILS because the
//	        synthetic sendFrame-shape body (no slices.Sort, args
//	        `err, encoderErr`) would now match. Codifies the
//	        positive-AND-negative discrimination property as a
//	        permanent structural invariant; complements M-VA-8
//	        positive-discrim and the M-VA-8 A/B empirical capture.
//
// Source-state tests are weaker than runtime fault-injection tests
// (Phase 4 owns U-1 through I-1 per Phase 2 design §6.1 + §6.2). They
// catch the structural-regression class: any future edit that strips
// the per-mediaType keying or the nil-deref guard breaks the build's
// "go test ./kernel/" surface, surfacing the regression at CI/dev
// time before the runtime defect ships.
//
// Spec anchor: Task #179 Phase 2 design md5 36a248b1 §3 mechanism +
// §3.5 interface contracts + §10 critique #3 (C1 nil-deref self-flag).
package kernel

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVectorA_PerMediaTypeDecoupling is the §3 source-state regression
// net. 12 subtests across three structural concerns (state partition,
// dispatch-site keying, C1 nil-deref guard + ordering, cycle-return
// aggregation + negative-discrimination) verify the per-mediaType
// mechanism is present, correctly ordered, and discriminated against
// other errors.Join sites in transcoder.go.
//
// Subtest naming convention: "<concern>/<element>" so subtest reports
// surface which concern + which structural element regressed.
//
// Empirical subtest count anchor: `grep -c "t\.Run(" kernel/vector_a_decoupling_test.go`
// at the cycle-return file (this test function = 12 subtests;
// TestVectorA_DispatchSites_DocAnchor = 2 subtests; total 14 — verify
// at SUBMIT time via the grep command verbatim, NOT buffer-recall).
func TestVectorA_PerMediaTypeDecoupling(t *testing.T) {
	body := readKernelSource(t, "transcoder.go")

	// State partition: the encoderErrors map declaration replaces
	// the prior `var encoderError error` scalar.
	t.Run("state/encoderErrors_map_declared", func(t *testing.T) {
		// Pattern: `encoderErrors := map[astiav.MediaType]error{}` OR
		// `encoderErrors := make(map[astiav.MediaType]error)`. Tolerate
		// either map-literal OR make-call construction.
		re := regexp.MustCompile(`encoderErrors\s*:?=\s*(?:map\[astiav\.MediaType\]error\{\}|make\(map\[astiav\.MediaType\]error\b)`)
		require.True(t, re.MatchString(body),
			"per-mediaType state-partition map `encoderErrors map[astiav.MediaType]error` "+
				"missing — Vector A architectural defect-fix regressed (M-VA-1)")
	})

	t.Run("state/scalar_encoderError_removed", func(t *testing.T) {
		// The OLD shared scalar `var encoderError error` must be GONE;
		// substring match is acceptable because Vector A's new symbol
		// is `encoderErrors` (with trailing 's') so a `var encoderError`
		// occurrence indicates regression to the shared-state ancestor.
		require.NotContains(t, body, "var encoderError error",
			"shared scalar `var encoderError error` resurrected — Vector A "+
				"per-mediaType decoupling regressed to single-state monotonic latch (M-VA-9)")
	})

	t.Run("state/locker_renamed", func(t *testing.T) {
		// Locker variable renamed to encoderErrorsLocker (mirrors map
		// name). Old `encoderErrorLocker` (singular) is the regression
		// indicator.
		require.Contains(t, body, "encoderErrorsLocker",
			"locker rename `encoderErrorsLocker` missing — paired with "+
				"map rename per Phase 2 design §3.1 + §3.5 thread-safety contract")
	})

	// Setter contract: mt parameter + first-wins-per-lane guard.
	t.Run("setter/mediatype_parameter", func(t *testing.T) {
		// Pattern: `setEncoderError := func(mt astiav.MediaType, err error)`
		re := regexp.MustCompile(`setEncoderError\s*:=\s*func\(\s*mt\s+astiav\.MediaType\s*,\s*err\s+error\s*\)`)
		require.True(t, re.MatchString(body),
			"setEncoderError closure must take `mt astiav.MediaType` as first param "+
				"per Phase 2 design §3.5 — without it, setter cannot key by lane (M-VA-2)")
	})

	t.Run("setter/first_wins_per_lane_guard", func(t *testing.T) {
		// Pattern: `if _, ok := encoderErrors[mt]; !ok {` followed by
		// `encoderErrors[mt] = err`.
		re := regexp.MustCompile(`(?s)if\s+_,\s*ok\s*:=\s*encoderErrors\[mt\];\s*!ok\s*\{[^}]*encoderErrors\[mt\]\s*=\s*err`)
		require.True(t, re.MatchString(body),
			"first-wins-per-lane guard `if _, ok := encoderErrors[mt]; !ok { encoderErrors[mt] = err }` "+
				"missing — INV-1 violated (last-wins replaces first-wins; M-VA-4)")
	})

	// Getter contract: mt parameter + lookup.
	t.Run("getter/mediatype_parameter", func(t *testing.T) {
		// Pattern: `getEncoderError := func(mt astiav.MediaType) error`
		re := regexp.MustCompile(`getEncoderError\s*:=\s*func\(\s*mt\s+astiav\.MediaType\s*\)\s+error`)
		require.True(t, re.MatchString(body),
			"getEncoderError closure must take `mt astiav.MediaType` param "+
				"per Phase 2 design §3.5 — without it, gate cannot consult lane (M-VA-3)")
	})

	// Dispatch-site keying: gates pass mediaType.
	t.Run("inner_dispatch/gate_mediatype_keyed", func(t *testing.T) {
		// Pattern: in the filterOutputCh inner-loop case, the gate
		// reads `getEncoderError(mt)`. The `mt` symbol is the
		// per-iteration lane variable (mt := out.GetMediaType()).
		// Non-greedy `.*?` across the case body (will stop at first
		// match, well before the next `case <-ticker.C:` boundary).
		re := regexp.MustCompile(`(?s)case out, ok := <-filterOutputCh:.*?getEncoderError\(mt\)`)
		require.True(t, re.MatchString(body),
			"inner-loop filterOutputCh gate must call `getEncoderError(mt)` "+
				"with per-iteration lane key — coupling regressed to no-arg call (M-VA-5)")
	})

	t.Run("outer_dispatch/gate_mediatype_keyed", func(t *testing.T) {
		// Pattern: outer dispatch loop uses f.GetMediaType() (already
		// in scope at L427-428 pre-Vector-A) as the lane key in the
		// gate. Match `getEncoderError(mt)` inside the func()-closure
		// body following `f := *out.Frame`.
		re := regexp.MustCompile(`(?s)f\s*:=\s*\*out\.Frame.*?getEncoderError\(mt\)`)
		require.True(t, re.MatchString(body),
			"outer-loop dispatch gate must call `getEncoderError(mt)` with the "+
				"frame's mediaType — coupling regressed to no-arg call (M-VA-6)")
	})

	t.Run("inner_dispatch/c1_nildref_guard_present", func(t *testing.T) {
		// C1 LOAD-BEARING: filterOutputCh inner-loop must guard against
		// empty OutputUnion (both Frame + Packet nil) before reaching
		// out.GetMediaType(). OutputUnion.Get() returns nil interface
		// when both are nil; subsequent .GetMediaType() panics on
		// method-call against nil interface.
		//
		// Pattern: `if out.Frame == nil && out.Packet == nil { continue }`
		// somewhere inside the filterOutputCh inner-loop case body
		// BEFORE `out.GetMediaType()` is invoked.
		re := regexp.MustCompile(`(?s)case out, ok := <-filterOutputCh:.*?if out\.Frame == nil && out\.Packet == nil \{.*?continue`)
		require.True(t, re.MatchString(body),
			"C1 LOAD-BEARING nil-deref guard `if out.Frame == nil && out.Packet == nil { continue }` "+
				"missing at inner filterOutputCh dispatch site — Vector A's mt:= out.GetMediaType() panic hazard "+
				"unguarded (M-VA-7); design §10 critique #3 self-flag — nil-deref panic regression")
	})

	// C3-impl ordering invariant: the C1 guard must appear strictly
	// BEFORE `mt := out.GetMediaType()` in the inner filterOutputCh
	// case. The previous c1_nildref_guard_present test only verifies
	// the guard's presence, NOT its position relative to
	// out.GetMediaType(). A regression that re-orders the guard AFTER
	// `mt := out.GetMediaType()` would re-introduce the panic hazard
	// (the guard would never gate the GetMediaType call) but
	// c1_nildref_guard_present alone would still pass.
	//
	// Anchor: locate the inner-loop filterOutputCh case offset, then
	// find the guard offset and the GetMediaType offset BOTH after
	// the case offset. Assert guard < GetMediaType.
	//
	// M-VA-10 broke-the-code: swap the order of the C1 guard and the
	// `mt := out.GetMediaType()` line at the inner filterOutputCh case
	// → this assertion fails because the guard now appears AFTER
	// GetMediaType in the body, defeating its panic-prevention purpose.
	t.Run("inner_dispatch/c1_nildref_guard_before_GetMediaType", func(t *testing.T) {
		caseAnchor := "case out, ok := <-filterOutputCh:"
		caseOffset := strings.Index(body, caseAnchor)
		require.GreaterOrEqual(t, caseOffset, 0,
			"filterOutputCh inner-loop case marker not found — test prerequisite failed")

		// Locate the guard's `if out.Frame == nil && out.Packet == nil`
		// substring AFTER the case marker.
		guardAnchor := "if out.Frame == nil && out.Packet == nil"
		guardOffset := strings.Index(body[caseOffset:], guardAnchor)
		require.GreaterOrEqual(t, guardOffset, 0,
			"C1 nil-deref guard substring missing inside filterOutputCh case (M-VA-10)")

		// Locate the `mt := out.GetMediaType()` AFTER the case marker.
		mtAnchor := "mt := out.GetMediaType()"
		mtOffset := strings.Index(body[caseOffset:], mtAnchor)
		require.GreaterOrEqual(t, mtOffset, 0,
			"`mt := out.GetMediaType()` not found inside filterOutputCh case — test prerequisite failed")

		require.Less(t, guardOffset, mtOffset,
			"C1 nil-deref guard must appear BEFORE `mt := out.GetMediaType()` "+
				"at filterOutputCh inner-loop — guard placed AFTER GetMediaType "+
				"defeats its purpose (panic hazard re-introduced; M-VA-10)")
	})

	// Cycle-return aggregation. Anchor on Vector A's specific
	// `slices.Sort(mediaTypes)` + `errors.Join(encErrs...)` idiom:
	// the sort gives deterministic ordering before the Join, and
	// `encErrs` is the named slice built from the per-mediaType map.
	// This anchor uniquely matches the Vector A cycle-return scope,
	// not other `errors.Join(...)` sites elsewhere in transcoder.go
	// (e.g. sendFrame's pre-existing `errors.Join(err, encoderErr)`
	// at the SendInput error path which has a different aggregation
	// shape). Earlier draft used `wg\.Wait\(\)[^{}]*errors\.Join\(`
	// which silently matched the wrong site (the `[^{}]*` clause
	// rejected the Vector A scope's intervening braces and instead
	// anchored on the brace-free pre-existing `errors.Join`).
	//
	// N1 BRITTLENESS NOTE: this anchor relies on `slices.Sort(mediaTypes)`
	// occurring exactly once in transcoder.go and being co-located with
	// the cycle-return scope. If a future refactor introduces a second
	// `slices.Sort(mediaTypes)` somewhere preceding sendFrame (or any
	// brace-free `errors.Join(encErrs...)` not anchored to Vector A),
	// the non-greedy `.*?` clause could match across scopes. Forward-
	// binding: prefer adding a tighter scope anchor (e.g.
	// `encoderErrorsLocker.Unlock()` immediately preceding) before
	// landing such a refactor. The negative-discrimination subtest
	// below codifies this property as a structural test invariant.
	//
	// M-VA-8 broke-the-code: comment out the cycle-return
	// `errors.Join(encErrs...)` aggregation → this assertion fails
	// because the `slices.Sort(mediaTypes) ... errors.Join(encErrs...)`
	// idiom no longer co-exists in source. Mutation captured A/B in
	// SUBMIT artifact (test FAIL pre-mutation-revert, PASS
	// post-revert). Falsifies INV-3 (errors.Is chain via
	// per-mediaType errors.Join aggregation).
	t.Run("cycle_return/errors_join_aggregation", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)slices\.Sort\(mediaTypes\).*?errors\.Join\(encErrs\.\.\.\)`)
		require.True(t, re.MatchString(body),
			"cycle-return aggregation missing the `slices.Sort(mediaTypes)` "+
				"+ `errors.Join(encErrs...)` Vector A idiom — per-mediaType errors "+
				"no longer aggregated with deterministic ordering; INV-3 violated (M-VA-8)")
	})

	// Negative-discrimination invariant: the M1 LOAD-BEARING fix's
	// regex must NOT match a body that contains an unrelated
	// `errors.Join(err, encoderErr)` shape (e.g. sendFrame's
	// pre-existing site at L584, which is brace-free between
	// wg.Wait and errors.Join — exactly what made the prior R1
	// regex `(?s)wg\.Wait\(\)[^{}]*errors\.Join\(` match it
	// vacuously). The Vector A-specific `encErrs...` argument name
	// + the `slices.Sort(mediaTypes)` precedence are the
	// discriminators: only the Vector A scope satisfies BOTH.
	//
	// This subtest codifies negative-discrim as a permanent
	// structural invariant — stronger than the empirical A/B
	// capture, which can become stale across refactors. The
	// synthetic negative body intentionally lacks the Vector A
	// idiom, mimicking the sendFrame shape; the regex must reject.
	//
	// M-VA-N (negative-discrim) broke-the-code: weaken the regex
	// back to `(?s)wg\.Wait\(\)[^{}]*errors\.Join\(` (or any pattern
	// not requiring both the slices.Sort anchor + encErrs argument
	// name) → this assertion fails because the synthetic sendFrame-
	// shape body would now match. Captures A/B in
	// ~/tmp/task179-evidence/r2.1/m-va-N-neg-discrim-{A,B}*.txt.
	t.Run("cycle_return/regex_negative_discrimination", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)slices\.Sort\(mediaTypes\).*?errors\.Join\(encErrs\.\.\.\)`)

		// Synthetic negative: sendFrame-shape body (no slices.Sort
		// of mediaTypes, errors.Join with different arg names).
		// This is the EXACT shape the prior R1 regex matched
		// vacuously — the v2 regex MUST reject it.
		sendFrameShape := `func sendFrame(ctx context.Context) error {
	wg.Wait()
	return errors.Join(err, encoderErr)
}`
		require.False(t, re.MatchString(sendFrameShape),
			"v2 regex must NOT match sendFrame-shape body (no slices.Sort + "+
				"errors.Join with different arg names) — negative-discrimination "+
				"invariant broken; regression to R1 vacuous-match class (M-VA-N)")

		// Positive control: actual canonical body DOES match.
		require.True(t, re.MatchString(body),
			"v2 regex must match the actual canonical Vector A cycle-return "+
				"body — positive control failed; test prerequisite broken")
	})
}

// TestVectorA_DispatchSites_DocAnchor pins the structural-test
// invariant in human-readable form. If the dispatch-site count or
// pattern shape changes (e.g. a third dispatch site is added without
// updating these tests), this anchor surfaces the gap explicitly.
func TestVectorA_DispatchSites_DocAnchor(t *testing.T) {
	body := readKernelSource(t, "transcoder.go")

	t.Run("setEncoderError_call_site_count", func(t *testing.T) {
		// Vector A has THREE setEncoderError call sites (mirrors
		// pre-Vector-A count, but each now carries the mt arg):
		//   (1) inner-loop after Encoder.SendInput error
		//   (2) outer-loop FilterKernel==nil branch after SendInput error
		//   (3) outer-loop FilterKernel!=nil branch after SendInput error
		// The closure declaration itself does NOT count (it's `:= func`,
		// not a call).
		callCount := strings.Count(body, "setEncoderError(mt,")
		require.Equal(t, 3, callCount,
			"setEncoderError call-site count expected 3 (matches pre-Vector-A "+
				"sites, now mt-keyed); got %d — dispatch-site enumeration drifted "+
				"from Phase 2 design §3.2 + §4.2", callCount)
	})

	t.Run("getEncoderError_call_site_count", func(t *testing.T) {
		// Vector A has TWO getEncoderError call sites:
		//   (1) inner-loop filterOutputCh gate
		//   (2) outer-loop func()-closure gate
		// The closure declaration itself does NOT count.
		callCount := strings.Count(body, "getEncoderError(mt)")
		require.Equal(t, 2, callCount,
			"getEncoderError call-site count expected 2 (inner gate + outer "+
				"gate); got %d — dispatch-site enumeration drifted", callCount)
	})
}
