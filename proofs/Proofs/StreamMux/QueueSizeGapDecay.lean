-- Proofs/StreamMux/QueueSizeGapDecay.lean: Correctness proofs for the
-- QueueSizeGapDecay auto-bitrate calculator

import Spec.StreamMux.QueueSizeGapDecay

open GapDecaySpec

/-! ## Property 1: Queue at optimal with zero derivative → bitRateDiff = 0

  When queueDuration = queueDurationOptimal and actualDerivative = 0,
  the gap is 0, so gapBytes = 0, desiredDerivative = 0, derivativeGap = 0,
  and bitRateDiff = 0.
-/

theorem bitRateDiff_zero_at_optimal_zero_deriv
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hQueue : req.queueDuration = queueDurationOptimal cfg req.actualOutputBitRate)
    (hDeriv : req.actualDerivative = 0) :
    computeBitRateDiff cfg req = 0 := by
  simp [computeBitRateDiff, gap, gapBytes, desiredDerivative, derivativeGap,
        bitRateDiff, hQueue, hDeriv]

/-! ## Property 2: Queue above optimal → bitrate decreases (bitRateDiff < 0)

  When the gap quotient (outputBps * gap / 8 / gapDecay) is strictly positive
  (i.e., survives integer division), the bitRateDiff is negative.
-/

theorem bitRateDiff_neg_above_optimal
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hDeriv : req.actualDerivative = 0)
    (hLargeGap : req.actualOutputBitRate *
        (req.queueDuration - queueDurationOptimal cfg req.actualOutputBitRate) /
        8 / cfg.gapDecay > 0) :
    computeBitRateDiff cfg req < 0 := by
  unfold computeBitRateDiff gap gapBytes desiredDerivative derivativeGap bitRateDiff
  simp only [hDeriv, Int.sub_zero]
  omega

/-! ## Property 3: Queue below optimal → bitrate increases (bitRateDiff > 0)

  When the gap quotient is strictly negative (queue below optimal, magnitude
  survives integer division), the bitRateDiff is positive.
-/

theorem bitRateDiff_pos_below_optimal
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hDeriv : req.actualDerivative = 0)
    (hLargeGap : req.actualOutputBitRate *
        (req.queueDuration - queueDurationOptimal cfg req.actualOutputBitRate) /
        8 / cfg.gapDecay < 0) :
    computeBitRateDiff cfg req > 0 := by
  unfold computeBitRateDiff gap gapBytes desiredDerivative derivativeGap bitRateDiff
  simp only [hDeriv, Int.sub_zero]
  omega

/-! ## Property 4: Inertia increase bound

  The result of applyInertia (for increases) never exceeds inertiaCapIncrease.
-/

theorem inertia_increase_bound
    (current checkInterval inertiaInc : Int)
    (raw : Int) (hRaw : raw > current) :
    applyInertia raw current checkInterval inertiaInc (1 : Int) ≤
      inertiaCapIncrease current checkInterval inertiaInc := by
  unfold applyInertia
  have hDiffPos : raw - current > 0 := by omega
  have hDiffNotNeg : ¬(raw - current < 0) := by omega
  simp only [hDiffPos, ite_true, hDiffNotNeg, ite_false]
  exact Int.min_le_right raw (inertiaCapIncrease current checkInterval inertiaInc)

/-! ## Property 5: Inertia decrease bound

  The result of applyInertia (for decreases) never goes below inertiaCapDecrease.
-/

theorem inertia_decrease_bound
    (current checkInterval inertiaDec : Int)
    (raw : Int) (hRaw : raw < current) :
    applyInertia raw current checkInterval (1 : Int) inertiaDec ≥
      inertiaCapDecrease current checkInterval inertiaDec := by
  unfold applyInertia
  have hDiffNotPos : ¬(raw - current > 0) := by omega
  have hDiffNeg : raw - current < 0 := by omega
  simp only [hDiffNotPos, ite_false, hDiffNeg, ite_true]
  exact Int.le_max_right raw (inertiaCapDecrease current checkInterval inertiaDec)

/-! ## Property 6: Critical flag semantics

  Critical is true iff the diff is negative AND the new bitrate is below
  max(actual, input) / 5.
-/

theorem critical_iff_decreasing_and_low
    (diff newBR actualOutput inputBR : Int) :
    isCritical diff newBR actualOutput inputBR = true ↔
      diff < 0 ∧ newBR < max actualOutput inputBR / 5 := by
  simp [isCritical, Bool.and_eq_true, decide_eq_true_eq]

theorem critical_false_when_increasing
    (diff newBR actualOutput inputBR : Int)
    (hPos : diff ≥ 0) :
    isCritical diff newBR actualOutput inputBR = false := by
  simp [isCritical]
  omega

/-! ## Additional structural properties -/

/-- rawNewBitRate is always at least 1. -/
theorem rawNewBitRate_ge_one (current diff : Int) :
    rawNewBitRate current diff ≥ 1 := by
  simp [rawNewBitRate]
  omega

/-- When bitRateDiff = 0, rawNewBitRate = max(currentBitRate, 1). -/
theorem rawNewBitRate_zero_diff (current : Int) :
    rawNewBitRate current 0 = max current 1 := by
  simp [rawNewBitRate]

/-- gap is zero when queueDuration equals optimal. -/
theorem gap_zero_at_optimal (queueDur optimal : Int)
    (h : queueDur = optimal) :
    gap queueDur optimal = 0 := by
  simp [gap, h]

/-- gapBytes is zero when gap is zero. -/
theorem gapBytes_zero_when_gap_zero (outputBps : Int) :
    gapBytes outputBps 0 = 0 := by
  simp [gapBytes]

/-- desiredDerivative is zero when gapBytes is zero. -/
theorem desiredDeriv_zero_when_gapBytes_zero (decayTime : Int) :
    desiredDerivative 0 decayTime = 0 := by
  simp [desiredDerivative]

/-- bitRateDiff is zero when derivativeGap is zero. -/
theorem bitRateDiff_zero_when_derivGap_zero :
    bitRateDiff 0 = 0 := by
  simp [bitRateDiff]

/-- derivativeGap is zero when both sides are equal. -/
theorem derivativeGap_self (v : Int) :
    derivativeGap v v = 0 := by
  simp [derivativeGap]

/-- applyInertia preserves the value when diff = 0. -/
theorem applyInertia_identity (current checkInterval inertiaInc inertiaDec : Int) :
    applyInertia current current checkInterval inertiaInc inertiaDec = current := by
  simp [applyInertia]

/-- Inertia cap for increases: capped value ≥ current (increase direction preserved). -/
theorem inertiaCapIncrease_ge_current
    (current checkInterval inertiaInc : Int)
    (hCur : current ≥ 0) (hInertia : inertiaInc > 0) (hInt : checkInterval ≥ 0) :
    inertiaCapIncrease current checkInterval inertiaInc ≥ current := by
  unfold inertiaCapIncrease
  have hSum : inertiaInc + checkInterval ≥ inertiaInc := by omega
  have hProd : current * (inertiaInc + checkInterval) ≥ current * inertiaInc := by
    exact Int.mul_le_mul_of_nonneg_left hSum hCur
  have hDiv : current * (inertiaInc + checkInterval) / inertiaInc ≥
              current * inertiaInc / inertiaInc := by
    exact Int.ediv_le_ediv (by omega) hProd
  have hSimp : current * inertiaInc / inertiaInc = current := by
    exact Int.mul_ediv_cancel current (by omega : inertiaInc ≠ 0)
  omega

/-- Critical flag is false when diff ≥ 0. -/
theorem not_critical_when_not_decreasing
    (diff newBR actualOutput inputBR : Int)
    (hNonNeg : diff ≥ 0) :
    isCritical diff newBR actualOutput inputBR = false := by
  simp [isCritical]
  omega

/-- Critical flag is false when newBR ≥ max(actual, input) / 5. -/
theorem not_critical_when_bitrate_high
    (diff newBR actualOutput inputBR : Int)
    (hHigh : newBR ≥ max actualOutput inputBR / 5) :
    isCritical diff newBR actualOutput inputBR = false := by
  simp [isCritical, Bool.and_eq_true, decide_eq_true_eq]
  omega
