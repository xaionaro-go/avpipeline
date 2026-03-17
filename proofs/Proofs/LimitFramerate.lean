-- Proofs/LimitFramerate.lean: Correctness proofs for LimitFramerate filter

import Spec.LimitFramerate

open LimitState

/-! ## Property 1: maxFPS.Num = 0 → all frames rejected -/

theorem zero_fps_rejects_all (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) :
    (s.match' inp minDur 0).accepted = false := by
  unfold LimitState.match'
  simp

/-! ## Property 2: First frame (initial state, pts ≥ 0) always accepted -/

theorem first_frame_accepted (inp : LimitState.Input) (minDur : Rational)
    (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hPTS : inp.pts ≥ 0) :
    (LimitState.init.match' inp minDur maxFPSNum).accepted = true := by
  unfold LimitState.init LimitState.match'
  simp only [beq_iff_eq, hFPS, ite_false]
  have h1 : (decide (inp.pts < (0 : Int) - 2) && decide ((0 : Int) ≥ 2)) = false := by
    simp only [show ¬((0 : Int) ≥ 2) from by omega, decide_false, Bool.and_false]
  simp only [h1, ite_false]
  have h2 : ¬(inp.pts < (0 : Int)) := by omega
  simp [h2]

/-! ## Property 3: Large backward jump rejected -/

theorem backward_jump_rejected (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hJump : inp.pts < s.minPTS - 2)
    (hMinPTS : s.minPTS ≥ 2) :
    (s.match' inp minDur maxFPSNum).accepted = false := by
  unfold LimitState.match'
  simp only [beq_iff_eq, hFPS, ite_false]
  have h1 : (decide (inp.pts < s.minPTS - 2) && decide (s.minPTS ≥ 2)) = true := by
    simp only [Bool.and_eq_true, decide_eq_true_eq]
    exact ⟨hJump, hMinPTS⟩
  simp [h1]

/-! ## Property 4: Debt exceeding 3 causes rejection -/

theorem excess_debt_rejected (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hNoJump : ¬(inp.pts < s.minPTS - 2 ∧ s.minPTS ≥ 2))
    (hBehind : inp.pts < s.minPTS)
    (hDebt : s.debt + (s.minPTS - inp.pts) > 3) :
    (s.match' inp minDur maxFPSNum).accepted = false := by
  unfold LimitState.match'
  simp only [beq_iff_eq, hFPS, ite_false, Bool.and_eq_true, decide_eq_true_eq,
    hNoJump, ite_false, hBehind, ite_true, hDebt]

/-! ## Property 5: Debt decreases when pts > minPTS -/

theorem debt_decreases_when_ahead (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hAhead : inp.pts > s.minPTS)
    (hDebt : s.debt > 0) :
    (s.match' inp minDur maxFPSNum).accepted = true →
    (s.match' inp minDur maxFPSNum).newState.debt < s.debt := by
  unfold LimitState.match'
  simp only [beq_iff_eq, hFPS, ite_false]
  have hNoJump : ¬(inp.pts < s.minPTS - 2 ∧ s.minPTS ≥ 2) := by omega
  have hNotBehind : ¬(inp.pts < s.minPTS) := by omega
  simp only [Bool.and_eq_true, decide_eq_true_eq, hNoJump, ite_false, hNotBehind]
  intro _
  split
  · omega
  · omega

/-! ## Property 6: NextMinPTS advances monotonically on acceptance -/

/-- Integer division advance: for a ≥ 0, n > 0, d > 0,
    quantizing a into frame IDs of size n/d and stepping to the next boundary
    yields a value ≥ a. -/
private theorem int_div_advance (a n d : Int) (_ha : a ≥ 0) (hn : n > 0) (hd : d > 0) :
    (a * d / n + 1) * n / d ≥ a := by
  apply Int.le_ediv_of_mul_le (by omega)
  have h1 : a * d / n * n ≤ a * d := Int.ediv_mul_le (a * d) (by omega)
  have hmod : a * d % n = a * d - n * (a * d / n) := Int.emod_def (a * d) n
  have hmod_lt : a * d % n < n := Int.emod_lt_of_pos _ (by omega)
  have key : (a * d / n + 1) * n = n * (a * d / n) + n := by
    have := Int.add_mul (a * d / n) 1 n
    simp at this
    have hc : a * d / n * n = n * (a * d / n) := Int.mul_comm (a * d / n) n
    omega
  omega

theorem next_min_pts_advances (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hMinDurNum : minDur.num > 0)
    (hMinDurDen : minDur.den > 0)
    (hPTS : inp.pts ≥ s.minPTS)
    (hMinPTSNonneg : s.minPTS ≥ 0) :
    (s.match' inp minDur maxFPSNum).accepted = true →
    (s.match' inp minDur maxFPSNum).newState.minPTS ≥ s.minPTS := by
  unfold LimitState.match'
  simp only [beq_iff_eq, hFPS, ite_false]
  have hNoJump : ¬(inp.pts < s.minPTS - 2 ∧ s.minPTS ≥ 2) := by omega
  have hNotBehind : ¬(inp.pts < s.minPTS) := by omega
  simp only [Bool.and_eq_true, decide_eq_true_eq, hNoJump, ite_false, hNotBehind]
  intro _
  have hMaxEq : max inp.pts s.minPTS = inp.pts := by omega
  simp only [hMaxEq]
  have h := int_div_advance inp.pts minDur.num minDur.den (by omega) hMinDurNum hMinDurDen
  omega

/-! ## Property 7: Duration remainder bounded by minDuration.Den -/

theorem dur_remainder_bounded (s : LimitState) (inp : LimitState.Input)
    (minDur : Rational) (maxFPSNum : Int)
    (hFPS : maxFPSNum ≠ 0)
    (hMinDurDen : minDur.den > 0)
    (hResult : (s.match' inp minDur maxFPSNum).accepted = true) :
    (s.match' inp minDur maxFPSNum).newState.durRemainder ≥ 0 ∧
    (s.match' inp minDur maxFPSNum).newState.durRemainder < minDur.den := by
  unfold LimitState.match' at hResult ⊢
  simp only [beq_iff_eq, hFPS, ite_false, Bool.and_eq_true, decide_eq_true_eq] at hResult ⊢
  by_cases hJump : inp.pts < s.minPTS - 2 ∧ s.minPTS ≥ 2
  · obtain ⟨h1, h2⟩ := hJump
    simp only [h1, h2, and_self, ite_true] at hResult
    contradiction
  · simp only [hJump, ite_false] at hResult ⊢
    by_cases hBehind : inp.pts < s.minPTS
    · simp only [hBehind, ite_true] at hResult ⊢
      by_cases hDebtHigh : s.debt + (s.minPTS - inp.pts) > 3
      · simp only [hDebtHigh, ite_true] at hResult
        contradiction
      · simp only [hDebtHigh, ite_false] at hResult ⊢
        exact ⟨Int.emod_nonneg _ (by omega), Int.emod_lt_of_pos _ (by omega)⟩
    · simp only [hBehind, ite_false] at hResult ⊢
      exact ⟨Int.emod_nonneg _ (by omega), Int.emod_lt_of_pos _ (by omega)⟩
