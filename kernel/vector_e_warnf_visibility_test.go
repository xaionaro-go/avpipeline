// vector_e_warnf_visibility_test.go is the regression net for the
// Vector E observability boost (Task #175 Phase 4 spec md5
// 2f54e30bd7533fcb53c6e1b1fceb2aa4). Vector E promoted 5 frame-drop
// sites in the cascade transcoder from Tracef / silent semantics to
// Warnf (and at one site, Errorf). Without these, operator logs at
// default level showed no trace of audio frame loss between phone-
// side production and consumer-side egress at /pixel/builtincamera-
// merged, blocking H1 (D-A3b mechanism) disambiguation.
//
// Realization (per spec §4.1 explicit "test-executor-1 elects
// realization" + coord W265 ratification of Option (a)+):
//
// All 5 sites tested here at the SOURCE-STATE level: each test reads
// the canonical kernel/*.go file at HEAD via git, locates the
// expected log call by symbol-anchored grep, and asserts:
//
//   1. The site emits at the expected level (Warnf for E1/E2/E3/E5;
//      Errorf for E4 — see "E4 level note" below).
//   2. The site does NOT emit at Tracef level (regression guard
//      against partial revert to pre-Vector-E silent-drop semantic).
//   3. The expected message-pattern substring is present at the site
//      (regression guard against accidental refactor that detaches
//      the cited message from the cited level).
//
// Source-state tests are weaker than runtime logger-capture tests
// (they cannot detect a refactor that moves the call to a code path
// that is never reached, for instance). They ARE strong against the
// regression class Vector E actually fears — silent-drop reversion
// at source. Runtime promotion is deferred to a follow-up cycle if
// a reviewer judges source-state coverage insufficient.
//
// The realization choice is documented per axis #20.2 and per coord
// W265 ratified hybrid framing. Per spec §4.1 realizability notes,
// runtime tests for E1/E2/E5 were also evaluated as feasible-with-
// substantial-cgo-setup and explicitly deferred to fit the Phase 4
// task budget that needs to also cover T-int-2..4 in this cycle.
//
// E4 level note: spec §4.1 + §6.4 declared E4 (DTS > PTS site at
// kernel/encoder.go) as a Warnf site. Empirical at canonical HEAD
// e6c63ba8 shows the site already emits at Errorf, NOT Warnf — see
// kernel/encoder.go:1070+1073. This is a known spec misframing
// tracked in Task #183 ("E4 misframing — empirical at canonical
// HEAD shows already-Errorf-not-silent-Tracef; doc amendment
// scope"). T-int-1 follows the testing-discipline rule "match
// shipped code, surface spec drift": E4 asserts Errorf. The spec
// amendment to align §6.4 wording is Task #183's scope.
//
// Per-test broke-the-code articulation (shared header):
//   - Reverting the Vector E commit at any of the 5 sites — i.e.,
//     restoring the pre-Vector-E Tracef OR removing the log call
//     entirely — causes the corresponding test below to FAIL because
//     the post-Vector-E level (Warnf or Errorf) would no longer be
//     present at the cited file:line.
//   - Demoting the Warnf/Errorf to Tracef would cause the
//     "no Tracef regression guard" assertion to FAIL.
//   - Detaching the message-pattern substring from the cited line
//     (e.g., refactoring the format string while preserving the call
//     site) would cause the message-match assertion to FAIL.
//
// Empirical broke-the-code captures are documented in the per-test
// docstrings + commit body; one mutation per E-site demonstrates
// the load-bearing falsification path.

package kernel

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// readKernelSource reads <repo-root>/kernel/<name> at the working-
// tree state. The Phase 4 tests verify the post-Vector-E source
// state at canonical HEAD e6c63ba8 — they are run against the same
// tree the spec ratifies.
func readKernelSource(t *testing.T, name string) string {
	t.Helper()
	// runtime.Caller resolves to this test file's path; the kernel/
	// dir holds the source files under test.
	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) must succeed")
	dir := filepath.Dir(here)
	path := filepath.Join(dir, name)
	body, err := os.ReadFile(path)
	require.NoError(t, err, "must read kernel/%s", name)
	return string(body)
}

// assertWarnfSiteWithPattern asserts that file <kernelFile> contains
// at least one `logger.Warnf(...)` call whose argument list contains
// `messageSubstr`, AND that no `logger.Tracef(...)` call at the same
// site contains the same substring (the Tracef-revert regression
// guard). The "site" boundary is necessarily approximate at source-
// state; the substring acts as a content-anchored locator.
func assertWarnfSiteWithPattern(t *testing.T, kernelFile, messageSubstr string) {
	t.Helper()
	body := readKernelSource(t, kernelFile)
	warnfRE := regexp.MustCompile(`logger\.Warnf\([^)]*` + regexp.QuoteMeta(messageSubstr))
	tracefRE := regexp.MustCompile(`logger\.Tracef\([^)]*` + regexp.QuoteMeta(messageSubstr))
	require.True(t, warnfRE.MatchString(body),
		"kernel/%s must contain logger.Warnf(...) with substring %q "+
			"(Vector E regression: site demoted to Tracef or removed)",
		kernelFile, messageSubstr)
	require.False(t, tracefRE.MatchString(body),
		"kernel/%s must NOT contain logger.Tracef(...) with substring %q "+
			"(Vector E regression: partial revert to pre-Vector-E silent-drop)",
		kernelFile, messageSubstr)
}

// assertErrorfSiteWithPattern is the Errorf-level analog of
// assertWarnfSiteWithPattern. Used for E4 (DTS > PTS) which is at
// Errorf level at canonical HEAD per Task #183.
func assertErrorfSiteWithPattern(t *testing.T, kernelFile, messageSubstr string) {
	t.Helper()
	body := readKernelSource(t, kernelFile)
	errorfRE := regexp.MustCompile(`logger\.Errorf\([^)]*` + regexp.QuoteMeta(messageSubstr))
	tracefRE := regexp.MustCompile(`logger\.Tracef\([^)]*` + regexp.QuoteMeta(messageSubstr))
	require.True(t, errorfRE.MatchString(body),
		"kernel/%s must contain logger.Errorf(...) with substring %q "+
			"(Vector E regression at E4 site)",
		kernelFile, messageSubstr)
	require.False(t, tracefRE.MatchString(body),
		"kernel/%s must NOT contain logger.Tracef(...) with substring %q "+
			"(Vector E regression: partial revert at E4 site)",
		kernelFile, messageSubstr)
}

// TestVectorE_E1_FitFrameDrop_EmitsWarnf — E1 site at
// kernel/encoder.go around L752 (encoder.SendFrame caller of
// streamEncoder.fitFrameForEncoding when the resampler returns zero
// frames during PCM warmup or format-mismatch transients).
//
// Spec §4.1 E1 message anchor: file-qualified prefix
// "kernel/encoder.go:fitFrameForEncoding". Pre-Vector-E behavior:
// silent drop (no log emit at any level), or Tracef during the
// encoder_resampler_aliasing investigation. Post-Vector-E: Warnf
// promotes the drop to operator-visible level.
//
// Broke-the-code:
//   - Reverting encoder.go L752 to Tracef → tracefRE matches → FAIL.
//   - Removing the file:func qualifier prefix → substring no longer
//     present → warnfRE no-match → FAIL.
func TestVectorE_E1_FitFrameDrop_EmitsWarnf(t *testing.T) {
	assertWarnfSiteWithPattern(t, "encoder.go",
		"kernel/encoder.go:fitFrameForEncoding: frame dropped")
}

// TestVectorE_E2_StallUnderThreshold_EmitsWarnf — E2 site at
// kernel/encoder_stall.go around L65 (handleEncoderStall, the
// av1_mediacodec stall watchdog under-threshold drop branch).
//
// Spec §4.1 E2 message anchor: "encoder stall under-threshold drop".
// Pre-Vector-E: silent shed of frames until the watchdog escalated
// to Reinit. Post-Vector-E: Warnf with mediaType discriminator so
// audio-vs-video stalls are disambiguable in operator logs.
//
// Broke-the-code:
//   - Reverting encoder_stall.go L65 to Tracef → FAIL.
//   - Removing the `mediaType=%s` discriminator → message-substring
//     match still passes (substring is the leading prefix), but the
//     mediaType-arg dropping is covered separately in #189 follow-up.
func TestVectorE_E2_StallUnderThreshold_EmitsWarnf(t *testing.T) {
	assertWarnfSiteWithPattern(t, "encoder_stall.go",
		"encoder stall under-threshold drop")
}

// TestVectorE_E3_DispatchSuppressing_EmitsWarnf — E3 site at
// kernel/transcoder.go around L413 (decoderToEncoder dispatch loop,
// still-suppressing message emitted periodically while encoderError
// is latched and frames continue to be skipped).
//
// Spec §4.1 E3 message anchor: "decoder→encoder dispatch still
// suppressing further skips". Pre-Vector-E: silent suppression after
// the first skip-Warnf was emitted, with no operator-visible cadence
// indicator that drops were continuing. Post-Vector-E: periodic
// re-emit with current count.
//
// Broke-the-code:
//   - Reverting transcoder.go L413 to Tracef → FAIL.
//   - Removing the periodic-re-emit branch entirely → substring
//     no longer present → FAIL.
func TestVectorE_E3_DispatchSuppressing_EmitsWarnf(t *testing.T) {
	// "decoder→encoder" uses U+2192 RIGHTWARDS ARROW; ensure substring
	// matches the exact source rune sequence.
	assertWarnfSiteWithPattern(t, "transcoder.go",
		"decoder→encoder dispatch still suppressing")
}

// TestVectorE_E4_DTSGreaterThanPTS_EmitsErrorf — E4 site at
// kernel/encoder.go around L1070 + L1073 (encoder packet output
// path, DTS > PTS detection branch).
//
// Spec §4.1 + §6.4 declared E4 a Warnf site. Empirical at canonical
// HEAD e6c63ba8 shows the site already emits at Errorf, not Warnf —
// see [T1: kernel/encoder.go:1069 read at e6c63ba8 this session +
// grep "DTS.*PTS.*Warnf|Errorf" returned Errorf only, high]. This
// is a known spec misframing tracked in Task #183 ("E4 misframing
// — empirical at canonical HEAD shows already-Errorf-not-silent-
// Tracef; doc amendment scope"). T-int-1 follows the testing-
// discipline rule "match shipped code, surface spec drift": this
// test asserts Errorf. Spec amendment to align §6.4 wording is
// Task #183's scope.
//
// E4 message anchor: "DTS (...) > PTS (...) skipping the packet".
// Pre-Vector-E (per spec narrative): silent skip. Post-Vector-E:
// Errorf with pict-type discriminator.
//
// Broke-the-code:
//   - Reverting encoder.go L1073 to Tracef → tracefRE matches → FAIL.
//   - Removing the Errorf branch entirely → errorfRE no-match → FAIL.
func TestVectorE_E4_DTSGreaterThanPTS_EmitsErrorf(t *testing.T) {
	assertErrorfSiteWithPattern(t, "encoder.go",
		") > PTS (")
}

// TestVectorE_E5_FilterOutputCh_EmitsWarnf — E5 site at
// kernel/transcoder.go around L365 (decoderToEncoder cycle's
// filterOutputCh first-Warnf when encoderError is already latched
// and the cycle is dropping frames).
//
// Spec §4.1 E5 message anchor: "filterOutputCh frames being
// dropped: encoderError already latched". Pre-Vector-E: silent
// drop (Tracef under verbose logging only). Post-Vector-E: Warnf
// with rate-limited re-emit per §6.3.
//
// E5 is the load-bearing site for H1 (D-A3b mechanism)
// disambiguation per phase2-design.md §10. Without E5 visible in
// AVD logs, the disambiguation tree row "(>0,0,0)" or "(>0,>0,0)
// + Vector E E5 marker present" cannot distinguish H1 from H2 (D-
// A4-NEW3 queue-full).
//
// Broke-the-code:
//   - Reverting transcoder.go L365 to Tracef → FAIL.
//   - Removing the rate-limited first-Warnf branch → substring
//     no longer present → FAIL.
func TestVectorE_E5_FilterOutputCh_EmitsWarnf(t *testing.T) {
	// Vector A (Task #179) inserted `(mediaType=%s)` between the
	// "frames being dropped" prefix and the colon. We anchor the
	// substring on BOTH the prefix and the suffix
	// (`encoderError already latched`) so a partial-revert that
	// keeps the prefix but drops the colon-suffix (e.g. degraded
	// observability message) still fails the assertion. Bypass the
	// shared `assertWarnfSiteWithPattern` helper because its
	// internal `[^)]*` regex clause cannot span the `):` between
	// the mediaType formatter and the rest of the message — use a
	// direct DOTALL-anchored regex instead.
	body := readKernelSource(t, "transcoder.go")
	suffixRE := regexp.MustCompile(`(?s)logger\.Warnf\([^,]*,\s*"filterOutputCh frames being dropped \(mediaType=%s\): encoderError already latched`)
	require.True(t, suffixRE.MatchString(body),
		"kernel/transcoder.go must contain logger.Warnf(...) with full E5 message "+
			`"filterOutputCh frames being dropped (mediaType=%%s): encoderError already latched" `+
			"(Vector E regression: site demoted, suffix dropped, or message format mutated)")
	// Tracef-revert dual-sided guard: same anchor must NOT appear at Tracef level.
	tracefRE := regexp.MustCompile(`(?s)logger\.Tracef\([^,]*,\s*"filterOutputCh frames being dropped \(mediaType=`)
	require.False(t, tracefRE.MatchString(body),
		"kernel/transcoder.go must NOT contain logger.Tracef(...) with `filterOutputCh frames being dropped (mediaType=` "+
			"(Vector E regression: partial revert to pre-Vector-E silent-drop)")
}

// TestVectorE_AllSites_NoTracefRevert is the dual-sided regression
// guard at file-scope. Vector E promoted these 5 sites away from
// Tracef / silent-drop semantics; if a refactor accidentally
// restored ANY of the per-site message-substrings at Tracef level,
// the corresponding per-site test above already catches it. This
// test additionally guards the DUAL message anchors: the "skipping
// the packet" prefix at E4, plus the file-qualified prefix
// "kernel/encoder.go:fitFrameForEncoding" at E1, must NEVER co-
// occur with a Tracef call in the same file. Defense-in-depth at
// the dual-sided gate.
//
// Broke-the-code:
//   - Adding a parallel Tracef call at any of the 5 sites (e.g.,
//     for diagnostic verbose-mode that bypasses the level-promotion)
//     → FAIL. This guards against a "Vector E preserved BUT a
//     parallel Tracef leaks at the same site" anti-pattern.
func TestVectorE_AllSites_NoTracefRevert(t *testing.T) {
	encoderBody := readKernelSource(t, "encoder.go")
	transcoderBody := readKernelSource(t, "transcoder.go")
	stallBody := readKernelSource(t, "encoder_stall.go")

	cases := []struct {
		body, anchor string
	}{
		{encoderBody, "kernel/encoder.go:fitFrameForEncoding"},
		{stallBody, "encoder stall under-threshold drop"},
		{transcoderBody, "decoder→encoder dispatch still suppressing"},
		{transcoderBody, "filterOutputCh frames being dropped"},
		// E4 anchors at the closing-paren-greater-paren prefix unique
		// to the DTS>PTS message; pure-Tracef revert at this site
		// would re-introduce the parser-friendly form.
		{encoderBody, ") > PTS ("},
	}
	for _, c := range cases {
		tracefRE := regexp.MustCompile(`logger\.Tracef\([^)]*` + regexp.QuoteMeta(c.anchor))
		require.False(t, tracefRE.MatchString(c.body),
			"Vector E regression: anchor %q must NOT appear at Tracef level "+
				"(Tracef-revert anti-pattern)", c.anchor)
	}

	// Cross-cut sanity check: the five anchors collectively occur in
	// the post-Vector-E source. If the package were refactored such
	// that one anchor moved to a different file, the per-site tests
	// would catch the relocation; this aggregate verifies the count
	// hasn't dropped to zero (smoke-level).
	totalCount := 0
	for _, c := range cases {
		totalCount += strings.Count(c.body, c.anchor)
	}
	require.GreaterOrEqual(t, totalCount, len(cases),
		"expected ≥%d Vector E anchor occurrences across encoder.go + "+
			"transcoder.go + encoder_stall.go; got %d (Vector E sites "+
			"may have been removed)", len(cases), totalCount)
}
