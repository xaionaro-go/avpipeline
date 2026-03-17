-- Proofs/MonotonicPTS.lean: Correctness proofs for MonotonicPTS filter

import Spec.MonotonicPTS

open MonotonicPTSState MatchResult

/-! ## PTS < DTS → rejected (regardless of other state) -/

theorem pts_lt_dts_rejected (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hLt : inp.pts < inp.dts) (hNotNoPTS : inp.dtsIsNoPTS = false) :
    s.matchResult inp tol = rejected := by
  simp [matchResult, ptsLtDts, hLt, hNotNoPTS]

/-! ## Non-first stream always accepted (streamIndex ≠ 0) -/

theorem non_first_stream_accepted (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hNonFirst : inp.streamIndex ≠ 0)
    (hValid : ¬ptsLtDts inp) :
    ∃ pts, s.matchResult inp tol = accepted pts := by
  simp [matchResult, hValid, hNonFirst]

/-! ## shouldCorrect=false, backward PTS → rejected -/

theorem no_correct_backward_rejected (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hBackward : ¬isForward s inp tol)
    (hNoCorrect : s.shouldCorrect = false) :
    s.matchResult inp tol = rejected := by
  simp [matchResult, hValid, hFirst, hBackward, hNoCorrect]

/-! ## shouldCorrect=true, backward PTS → accepted with corrected PTS -/

theorem correct_backward_accepted (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hBackward : ¬isForward s inp tol)
    (hCorrect : s.shouldCorrect = true) :
    s.matchResult inp tol = accepted (s.latestPTS + 1) := by
  simp [matchResult, hValid, hFirst, hBackward, hCorrect]

/-! ## After correction, shift stored = (latestPTS + 1) - originalPTS -/

theorem correction_shift_stored (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hBackward : ¬isForward s inp tol)
    (hCorrect : s.shouldCorrect = true) :
    (s.matchNextState inp tol).sourcePTSShift inp.sourceKey =
      s.latestPTS + 1 - inp.pts := by
  simp [matchNextState, hValid, hFirst, hBackward, hCorrect]

/-! ## LatestPTS bounded non-decreasing across any match call.

The Go code allows `latestPTS` to decrease by up to `tolerance` when a frame
within the tolerance window is accepted (it sets latestPTS = pts even if
pts < latestPTS, as long as pts + tolerance > latestPTS). So the true
invariant is: latestPTS never decreases by more than `tolerance`. -/

theorem latestPTS_bounded_decrease (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hTolNonneg : tol ≥ 0) :
    (s.matchNextState inp tol).latestPTS ≥ s.latestPTS - tol := by
  unfold matchNextState
  split
  · -- PTS < DTS: state unchanged
    omega
  · -- Valid PTS/DTS, non-first stream: state unchanged
    split
    · omega
    · -- First stream
      split
      · -- forward: latestPTS := shiftedPTS, shiftedPTS + tol > latestPTS
        rename_i _ _ hFwd
        simp [isForward, shiftedPTS] at hFwd
        simp [shiftedPTS]
        omega
      · -- backward
        split
        · -- no correction: state unchanged
          omega
        · -- correction: latestPTS := latestPTS + 1
          simp only
          omega

/-! ## With zero tolerance, latestPTS is strictly non-decreasing -/

theorem latestPTS_non_decreasing_zero_tol (s : MonotonicPTSState) (inp : FrameInput) :
    (s.matchNextState inp 0).latestPTS ≥ s.latestPTS := by
  have h := latestPTS_bounded_decrease s inp 0 (by omega)
  omega

/-! ## Non-first stream does not change latestPTS -/

theorem non_first_preserves_latestPTS (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hNonFirst : inp.streamIndex ≠ 0)
    (hValid : ¬ptsLtDts inp) :
    (s.matchNextState inp tol).latestPTS = s.latestPTS := by
  simp [matchNextState, hValid, hNonFirst]

/-! ## Forward PTS on first stream updates latestPTS to the shifted PTS -/

theorem forward_updates_latestPTS (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hForward : isForward s inp tol) :
    (s.matchNextState inp tol).latestPTS = s.shiftedPTS inp := by
  simp [matchNextState, hValid, hFirst, hForward]

/-! ## Correction sets latestPTS to latestPTS + 1 -/

theorem correction_updates_latestPTS (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hBackward : ¬isForward s inp tol)
    (hCorrect : s.shouldCorrect = true) :
    (s.matchNextState inp tol).latestPTS = s.latestPTS + 1 := by
  simp [matchNextState, hValid, hFirst, hBackward, hCorrect]

/-! ## DTS = NoPtsValue bypasses PTS < DTS check -/

theorem nopts_dts_bypasses_check (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hNoPTS : inp.dtsIsNoPTS = true)
    (hNonFirst : inp.streamIndex ≠ 0) :
    ∃ pts, s.matchResult inp tol = accepted pts := by
  simp [matchResult, ptsLtDts, hNoPTS, hNonFirst]

/-! ## Correction strictly increases latestPTS -/

theorem correction_strictly_increases (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hValid : ¬ptsLtDts inp)
    (hFirst : inp.streamIndex = 0)
    (hBackward : ¬isForward s inp tol)
    (hCorrect : s.shouldCorrect = true) :
    (s.matchNextState inp tol).latestPTS > s.latestPTS := by
  rw [correction_updates_latestPTS s inp tol hValid hFirst hBackward hCorrect]
  omega

/-! ## Rejected frames do not change state -/

theorem rejected_preserves_state (s : MonotonicPTSState) (inp : FrameInput) (tol : Int)
    (hRej : s.matchResult inp tol = rejected) :
    s.matchNextState inp tol = s := by
  unfold matchResult at hRej
  unfold matchNextState
  -- Both use the same if-chain; split on the shared conditions
  split
  · rfl
  · split
    · simp_all
    · split
      · simp_all
      · split
        · rfl
        · simp_all
