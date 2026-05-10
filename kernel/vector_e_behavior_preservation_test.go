// vector_e_behavior_preservation_test.go is the source-state regression
// net for the Vector E §6.4 "no behavior change" claim. Vector E
// commits added log infrastructure (Warnf at 5 sites + rate-limiter
// state machine at E3+E5); §6.4 asserts that this addition does NOT
// change packet-flow behavior — pre-Vector-E (silent drop) and
// post-Vector-E (logged drop) produce byte-identical packet output
// for the same input.
//
// Spec anchor: Task #175 Phase 4 spec md5 2f54e30bd7533fcb53c6e1b1fceb2aa4
// §4.3 T-int-3 — behavior preservation A/B (MINIMAL spec, MEDIUM
// priority).
//
// Realization (per coord W265 source-level all-source-level
// ratification, consistent with T-int-1 + T-int-2 pattern):
//
// Spec §4.3 sketches a runtime A/B differential (Run A with Vector E
// commits, Run B with `git stash`-reverted commits, byte-compare
// packet output). Source-state realization here checks structural
// invariants that — if held — guarantee behavior preservation
// without runtime cost:
//
//	INV1: Drop-decision is guarded ONLY by `getEncoderError() != nil`,
//	      not by log state (errAlreadyLogged etc).
//	INV2: `continue` placement after log block is unconditional —
//	      log state does not gate the drop.
//	INV3: No `logger.IsLevel*` checks in drop branches (would mean
//	      log level changes packet-flow control).
//	INV4: No `time.Sleep` in drop branches (would inject non-
//	      deterministic delay, potentially altering concurrent
//	      drain timing — spec §4.3 broke-the-code "non-deterministic
//	      counter" symmetric source pattern).
//	INV5: No `math/rand` references (would inject non-determinism).
//	INV6: No assignment-form `<var> = logger.Warnf(...)` (Vector E
//	      log calls must be void emits; assignment would feed values
//	      into surrounding control flow).
//
// If all 6 INVs hold, the Vector E source state contains LOG-ONLY
// additions wrt packet flow — preserving pre-Vector-E silent-drop
// semantic at the observable-output level (drop happens; only the
// log emission differs).
//
// Source-state tests are weaker than runtime A/B differential
// (cannot detect a refactor that moves the call to a code path that
// is never reached, for instance). They ARE strong against the
// regression class §6.4 actually fears — log infrastructure leaking
// into packet-flow control. Runtime A/B differential promotion is
// explicitly deferred per spec §4.3 realizability notes (existing
// pcap-capture fixture absent in kernel/; mock-sink approach at
// runtime would require full Transcoder fixture with synthetic
// frames, ~2-4h cost vs §4.3 MEDIUM-priority MINIMAL-spec ETA budget).
//
// Per-test broke-the-code articulation (shared header — per-subtest
// articulation in subtest docstrings):
//
//	M-BP-1: insert `if logger.IsLevelTrace { ... }` in drop branch →
//	        global/no_log_level_guards_in_dropsites assertion FAILS
//	        (matches spec §4.3 broke-the-code "conditionally skip a
//	        drop-site BASED on log-level → A/B differential bytes
//	        differ"; source-state form: log-level-check presence)
//
//	M-BP-2: insert `time.Sleep(time.Microsecond)` between droppedCount++
//	        and continue → global/no_time_sleep_in_dropsites assertion
//	        FAILS (matches spec "inject randomness via Vector E (non-
//	        deterministic counter) → A/B count differs"; source-state
//	        form: timing-sink presence)
//
//	M-BP-3: change drop guard from `if err := getEncoderError(); err
//	        != nil { droppedCount++` to `if err := getEncoderError();
//	        err != nil && !errAlreadyLogged { droppedCount++` → E5/
//	        drop_decision_log_independent assertion FAILS (drop policy
//	        now depends on log state — direct behavior change)
//
//	M-BP-4: replace `continue` after log block with `if
//	        errAlreadyLogged { continue }` → E5/continue_unconditional_
//	        after_log assertion FAILS (continue gated by log state —
//	        direct behavior change)
//
// All 4 mutations empirically captured this session as A/B differential
// (path-(a)) — see commit body for per-mutation pre/post/restore
// exit codes. Per IF-tx2-1 lesson learned at T-int-2 R1: option A
// (capture-all) elected over option B (symmetry-mitigation).

package kernel

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVectorE_BehaviorPreservation_NoPacketDelta verifies the 6
// structural invariants from the file header. 8 subtests cover the
// 6 INVs across the 3 kernel files Vector E modified
// (transcoder.go + encoder.go + encoder_stall.go).
//
// Subtest naming: "<scope>/<invariant>" so failure reports surface
// which structural invariant regressed.
//
// Spec §4.3 GOOD-side maps to "all 6 INVs hold across 3 files":
// drop-flow preserved, log infrastructure side-effect-only, no
// timing/randomness, no log-level gates.
//
// Spec §4.3 BAD-side maps to "any INV violated": each violation
// caught by exactly one (or two related) subtest. Localization by
// subtest name surfaces which invariant regressed.
func TestVectorE_BehaviorPreservation_NoPacketDelta(t *testing.T) {
	transcoderBody := readKernelSource(t, "transcoder.go")
	encoderBody := readKernelSource(t, "encoder.go")
	stallBody := readKernelSource(t, "encoder_stall.go")

	files := []struct {
		name, body string
	}{
		{"transcoder.go", transcoderBody},
		{"encoder.go", encoderBody},
		{"encoder_stall.go", stallBody},
	}

	// INV3: no logger.IsLevel* checks in any of the 3 Vector E files.
	t.Run("global/no_log_level_guards_in_dropsites", func(t *testing.T) {
		for _, f := range files {
			require.NotContains(t, f.body, "logger.IsLevel",
				"%s must not contain logger.IsLevel calls "+
					"(Vector E behavior change: log level gating control flow — M-BP-1)",
				f.name)
		}
	})

	// INV4: no time.Sleep in drop sites (transcoder.go is the only
	// kernel file with rate-limiter drop branches; encoder.go +
	// encoder_stall.go have point Warnf/Errorf without sleep-gated
	// drop logic).
	t.Run("global/no_time_sleep_in_dropsites", func(t *testing.T) {
		require.NotContains(t, transcoderBody, "time.Sleep",
			"transcoder.go must not contain time.Sleep "+
				"(Vector E behavior change: non-deterministic delay in drop sites — M-BP-2)")
	})

	// INV5: no math/rand references (would inject non-determinism
	// into packet flow). Check both import-form and use-form.
	t.Run("global/no_rand_references", func(t *testing.T) {
		for _, f := range files {
			require.NotContains(t, f.body, `"math/rand"`,
				"%s must not import math/rand "+
					"(Vector E behavior change: non-deterministic counter)", f.name)
			// Use-form: `rand.Intn(`, `rand.Float`, etc. Anchored prefix to
			// avoid false-positive on unrelated identifiers like "random".
			randUseRE := regexp.MustCompile(`\brand\.[IFN]`)
			require.False(t, randUseRE.MatchString(f.body),
				"%s must not call math/rand functions "+
					"(Vector E behavior change: non-deterministic counter)", f.name)
		}
	})

	// INV6: Warnf calls must be void emits — no assignment-form
	// capture of return value. Pattern: `<var> := logger.Warnf(` or
	// `<var> = logger.Warnf(`. logger.Warnf actually returns nothing
	// useful; assignment would suggest a refactor that captures it
	// for control-flow purposes.
	t.Run("transcoder/no_log_assignment", func(t *testing.T) {
		// Match `<ident><whitespace>:= logger.Warnf(` and `<ident>... = logger.Warnf(`.
		assignRE := regexp.MustCompile(`[a-zA-Z_]\w*\s+:=\s+logger\.Warnf\(`)
		require.False(t, assignRE.MatchString(transcoderBody),
			"transcoder.go must not assign-form logger.Warnf return value "+
				"(Vector E log calls must be void emits)")
		assignEqRE := regexp.MustCompile(`[a-zA-Z_]\w*\s*=\s*logger\.Warnf\(`)
		require.False(t, assignEqRE.MatchString(transcoderBody),
			"transcoder.go must not = logger.Warnf return value "+
				"(Vector E log calls must be void emits)")
	})

	// INV1 (E5): drop guard is `if err := getEncoderError(mt); err !=
	// nil { droppedCount++ ...`. Tightening: droppedCount++ must
	// IMMEDIATELY follow the encoderError check (no log-state in the
	// guard condition itself; drop happens regardless of log state).
	// Vector A (Task #179) added the per-iteration `mt` lane key so
	// the gate consults the correct mediaType lane; behavior at
	// per-lane scope is unchanged from Vector E's pre-decoupling
	// invariant.
	t.Run("E5/drop_decision_log_independent", func(t *testing.T) {
		// Pattern: `if err := getEncoderError(mt); err != nil {<whitespace>droppedCount++`
		// allowing only whitespace between `{` and `droppedCount++` —
		// no log-state in guard, no other intervening logic.
		re := regexp.MustCompile(`(?s)if err := getEncoderError\(mt\); err != nil \{\s+droppedCount\+\+`)
		require.True(t, re.MatchString(transcoderBody),
			"E5 drop guard must be `if err := getEncoderError(mt); err != nil { droppedCount++` "+
				"with droppedCount++ immediately after — log state must NOT mix into drop guard "+
				"(Vector E behavior change: log state in drop guard — M-BP-3)")
	})

	// INV2 (E5): continue after log block is unconditional. Pattern:
	// `if !errAlreadyLogged { ... }<whitespace>continue<whitespace>}`.
	// The `continue` is at the SAME nesting level as the inner if,
	// not inside it; and not gated by errAlreadyLogged.
	t.Run("E5/continue_unconditional_after_log", func(t *testing.T) {
		// Match: closing brace of `if !errAlreadyLogged { ... }` then
		// whitespace then `continue` then whitespace then closing brace
		// of the outer `if err := getEncoderError(); err != nil { ... }`.
		re := regexp.MustCompile(`(?s)errAlreadyLogged = true\s+\}\s+continue\s+\}`)
		require.True(t, re.MatchString(transcoderBody),
			"E5 `continue` must be unconditional after `if !errAlreadyLogged { ... }` block "+
				"(Vector E behavior change: log state gating drop-continue — M-BP-4)")
	})

	// INV1 (E3): symmetric structural invariant for resultCh dispatch
	// loop. Same pattern with resultDroppedCount + resultErrAlreadyLogged
	// + per-iteration `mt` lane key (Vector A Task #179).
	t.Run("E3/drop_decision_log_independent", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)if err := getEncoderError\(mt\); err != nil \{\s+resultDroppedCount\+\+`)
		require.True(t, re.MatchString(transcoderBody),
			"E3 drop guard must be `if err := getEncoderError(mt); err != nil { resultDroppedCount++` "+
				"with resultDroppedCount++ immediately after")
	})

	// INV2 (E3): drop-terminator placement preserved at dispatch loop
	// drop site. NOTE: E3 uses `return` not `continue` because the
	// drop logic lives inside an inline `func() { defer ...; if ... {
	// return } ... }()` block (resource-cleanup discipline). The
	// behavior preservation invariant is identical: `return` (drop)
	// must be unconditional after the log block, not gated by log
	// state. Pattern: closing `}` of inner if, then whitespace, then
	// `return`, then whitespace, then closing `}` of outer if-guard.
	t.Run("E3/return_unconditional_after_log", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)resultErrAlreadyLogged = true\s+\}\s+return\s+\}`)
		require.True(t, re.MatchString(transcoderBody),
			"E3 `return` (drop terminator inside inline func) must be unconditional "+
				"after `if !resultErrAlreadyLogged { ... }` block "+
				"(Vector E behavior change: log state gating drop-return)")
	})
}
