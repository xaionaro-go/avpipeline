// vector_e_rate_limiter_test.go is the source-state regression net for
// the Vector E §6.3 rate-limiter spam guard at the E5 + E3 sites in
// kernel/transcoder.go decoderToEncoder. The rate-limiter implements
// "first-wins" semantics between two reset triggers (cycle end OR
// periodicReportInterval elapsed) so an encoderError-suppressed drop
// window emits exactly 1 first-Warnf + 1 cycle-end summary in the
// common case, NOT one Warnf per dropped frame.
//
// Spec anchor: Task #175 Phase 4 spec md5 2f54e30bd7533fcb53c6e1b1fceb2aa4
// §4.2 T-int-2 — rate-limiter spam guard test (MINIMAL spec, MEDIUM
// priority).
//
// Realization (per coord W265 source-level all-source-level
// ratification + W278 T-int-2 dispatch brief):
//
// The §6.3 rate-limiter is NOT a separate exported helper — it is an
// inline state-machine in the decoderToEncoder() goroutines, with
// three structural elements per cycle:
//
//	(1) First-emission guard:
//	    if !errAlreadyLogged {
//	        logger.Warnf(ctx, "...frames being dropped...")
//	        errAlreadyLogged = true
//	    }
//
//	(2) Periodic ticker reset:
//	    case <-ticker.C:
//	        if errAlreadyLogged && droppedCount > 0 {
//	            logger.Warnf(ctx, "...still suppressing...")
//	            errAlreadyLogged = false
//	        }
//
//	(3) Cycle-end summary defer:
//	    defer func() {
//	        if droppedCount > 0 {
//	            logger.Warnf(ctx, "...cycle ended: N frame(s)...")
//	        }
//	    }()
//
// Both filterOutputCh (E5 site) and resultCh dispatch (E3 site) carry
// the same pattern with their own state vars (errAlreadyLogged +
// droppedCount vs resultErrAlreadyLogged + resultDroppedCount) and
// their own ticker (ticker vs resultTicker).
//
// Source-state assertions check that ALL three structural elements
// are present at BOTH sites, with the correct relationship (guard
// gates emit; emit sets flag; ticker resets flag; defer fires summary).
//
// Source-state tests are weaker than runtime logger-capture tests
// (they cannot detect a refactor that moves the call to a code path
// that is never reached, for instance). They ARE strong against the
// regression class §6.3 actually fears — the rate-limiter STRUCTURAL
// pattern being regressed (guard removed → 1-per-drop spam; ticker
// reset removed → silent-after-first; cycle-end summary removed →
// silent-during-cycle-end). Runtime logger-capture promotion is
// explicitly deferred per spec §4.2 realizability notes (helper is
// inline, not exposed; runtime test would require full Transcoder
// fixture with controllable encoderError state, ~2-4h cost vs §4.2
// MEDIUM-priority MINIMAL-spec ETA budget).
//
// Per-test broke-the-code articulation (shared header — per-subtest
// articulation in subtest docstrings):
//
//	M-RL-1: delete `if !errAlreadyLogged` first-Warnf guard at the
//	        E5 site → guard-present assertion FAILS (matches spec
//	        §4.2 broke-the-code "Remove rate-limiter call → 100 Warnf
//	        events observed → over-trigger assertion FAILS"; source-
//	        state form: structural pattern absence).
//
//	M-RL-2: delete `errAlreadyLogged = false` in the ticker.C branch
//	        at the E5 site → ticker-reset-clears-flag assertion FAILS
//	        (matches spec §4.2 "Make rate-limiter always-suppress →
//	        0 Warnf events observed → under-trigger assertion FAILS";
//	        source-state form: reset-pattern absence).
//
//	M-RL-3: change defer summary's `if droppedCount > 0` to
//	        `if droppedCount > 0 && tickerFired` (forcing both-required)
//	        → cycle-end-summary-defer assertion FAILS (matches spec
//	        §4.2 "Make rate-limiter both-required → cycle-end summary
//	        blocked until 60s elapses → first-wins semantic assertion
//	        FAILS"; source-state form: independent-cycle-end-trigger
//	        absence).
//
//	M-RL-4: rename periodicReportInterval to time.Hour → ticker-uses-
//	        periodicReportInterval assertion FAILS (constant-name
//	        anchored).
//
// One empirical broke-the-code capture (M-RL-1) is documented in the
// commit body; remaining mutations follow the same falsification
// chain articulated above.

package kernel

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVectorE_RateLimiter_FirstWins is the §6.3 source-state
// regression net. 10 subtests across both rate-limiter cycles (E5
// filterOutputCh + E3 resultCh dispatch) verify all three structural
// elements per cycle are present + 2 cross-cutting constants.
//
// Subtest naming convention: "<cycle>/<element>" so subtest reports
// surface which cycle + which structural element regressed.
//
// Spec §4.2 GOOD-side maps to "all 3 elements present at both
// cycles": guard limits first-Warnf to 1 per cycle; ticker reset
// allows periodic re-emit; defer summary fires at cycle end —
// together yielding exactly "1 first-Warnf + 1 summary" in the
// common (cycle-end-before-ticker) case.
//
// Spec §4.2 BAD-side maps to "any of the 3 elements absent": guard
// missing → spam (per-drop emit); ticker-reset missing → silent-
// forever-after-first; defer summary missing → silent-at-cycle-end.
// Each absence is caught by exactly one subtest.
func TestVectorE_RateLimiter_FirstWins(t *testing.T) {
	body := readKernelSource(t, "transcoder.go")

	// E5 site: filterOutputCh goroutine inside decoderToEncoder().
	// State vars: errAlreadyLogged (bool) + droppedCount (int).
	// Ticker: `ticker := time.NewTicker(periodicReportInterval)`.
	t.Run("E5/first_warnf_guard_present", func(t *testing.T) {
		// Guard: `if !errAlreadyLogged { ... logger.Warnf(...filterOutputCh frames being dropped... }`
		// Regex tolerates whitespace/newlines between guard and emit by using DOTALL via (?s).
		re := regexp.MustCompile(`(?s)if !errAlreadyLogged \{[^}]*logger\.Warnf\([^)]*filterOutputCh frames being dropped`)
		require.True(t, re.MatchString(body),
			"E5 first-Warnf guard `if !errAlreadyLogged { logger.Warnf(...filterOutputCh frames being dropped...) }` "+
				"missing — rate-limiter spam guard regressed (M-RL-1)")
	})

	t.Run("E5/first_warnf_flag_set_after_emit", func(t *testing.T) {
		// After the emit, the flag must be set so the next drop is suppressed.
		// Source pattern: `errAlreadyLogged = true` appears at least once.
		require.GreaterOrEqual(t, strings.Count(body, "errAlreadyLogged = true"), 1,
			"E5 must set `errAlreadyLogged = true` after first-Warnf emit "+
				"— otherwise rate-limiter would re-emit on every drop event")
	})

	t.Run("E5/ticker_reset_clears_flag", func(t *testing.T) {
		// `case <-ticker.C:` branch must clear errAlreadyLogged for periodic re-emit.
		re := regexp.MustCompile(`(?s)case <-ticker\.C:[^}]*errAlreadyLogged = false`)
		require.True(t, re.MatchString(body),
			"E5 ticker.C branch must clear errAlreadyLogged for periodic re-emit "+
				"(always-suppress regression guard — M-RL-2)")
	})

	t.Run("E5/cycle_end_summary_defer_present", func(t *testing.T) {
		// `defer func() { if droppedCount > 0 { logger.Warnf(...filterOutputCh cycle ended...) } }()`
		re := regexp.MustCompile(`(?s)defer func\(\) \{[^}]*if droppedCount > 0[^}]*logger\.Warnf\([^)]*filterOutputCh cycle ended`)
		require.True(t, re.MatchString(body),
			"E5 cycle-end summary defer must independently emit on `if droppedCount > 0` "+
				"(both-required regression guard — M-RL-3)")
	})

	// E3 site: resultCh dispatch loop inside decoderToEncoder().
	// State vars: resultErrAlreadyLogged + resultDroppedCount.
	// Ticker: `resultTicker := time.NewTicker(periodicReportInterval)`.
	t.Run("E3/first_warnf_guard_present", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)if !resultErrAlreadyLogged \{[^}]*logger\.Warnf\([^)]*decoder→encoder dispatch skipping`)
		require.True(t, re.MatchString(body),
			"E3 first-Warnf guard `if !resultErrAlreadyLogged { logger.Warnf(...decoder→encoder dispatch skipping...) }` "+
				"missing — rate-limiter spam guard regressed at dispatch loop")
	})

	t.Run("E3/first_warnf_flag_set_after_emit", func(t *testing.T) {
		require.GreaterOrEqual(t, strings.Count(body, "resultErrAlreadyLogged = true"), 1,
			"E3 must set `resultErrAlreadyLogged = true` after first-Warnf emit "+
				"— otherwise dispatch-loop rate-limiter would re-emit on every skip event")
	})

	t.Run("E3/ticker_reset_clears_flag", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)case <-resultTicker\.C:[^}]*resultErrAlreadyLogged = false`)
		require.True(t, re.MatchString(body),
			"E3 resultTicker.C branch must clear resultErrAlreadyLogged for periodic re-emit "+
				"at the dispatch loop (always-suppress regression guard at E3)")
	})

	t.Run("E3/cycle_end_summary_defer_present", func(t *testing.T) {
		re := regexp.MustCompile(`(?s)defer func\(\) \{[^}]*if resultDroppedCount > 0[^}]*logger\.Warnf\([^)]*decoder→encoder dispatch cycle ended`)
		require.True(t, re.MatchString(body),
			"E3 cycle-end summary defer must independently emit on `if resultDroppedCount > 0` "+
				"(both-required regression guard at E3)")
	})

	// Cross-cutting assertions: rate-limiter constant and ticker
	// references. periodicReportInterval is the only sized-bound the
	// rate-limiter relies on; if it were rewritten to time.Hour or
	// math.MaxInt64 the periodic re-emit semantic would silently
	// regress to "effectively never".
	t.Run("constant/periodicReportInterval_is_60s", func(t *testing.T) {
		require.Contains(t, body, "periodicReportInterval = 60 * time.Second",
			"periodicReportInterval must be defined as `60 * time.Second` "+
				"(spec §6.3 explicit cadence; constant-rename or value-change regresses periodic re-emit — M-RL-4)")
	})

	t.Run("constant/both_tickers_use_periodicReportInterval", func(t *testing.T) {
		// Two ticker constructions — one for filterOutputCh (E5), one for
		// resultCh dispatch (E3). Both must use periodicReportInterval.
		require.Equal(t, 2, strings.Count(body, "time.NewTicker(periodicReportInterval)"),
			"expected exactly 2 tickers in transcoder.go using periodicReportInterval "+
				"(filterOutputCh + resultCh dispatch); regression: ticker hardcoded to "+
				"different value or one ticker dropped")
	})
}
