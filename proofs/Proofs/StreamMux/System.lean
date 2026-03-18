-- Proofs/StreamMux/System.lean: System-level invariant proofs for the
-- streammux auto-bitrate control loop state machine.
--
-- Proves 6 cross-cutting invariants:
-- INV1: Numerical Safety (non-zero denominators in computation chain)
-- INV2: Data Flow Correctness (audio reads audio, not video)
-- INV3: Resolution Stability (slowdown enforces minimum interval)
-- INV4: Derivative Bounded (integer division truncation)
-- INV5: Bitrate Convergence (equilibrium property)
-- INV6: Measurement Monotonicity (convex combination through measurement loop)

import Spec.StreamMux.System
import Proofs.StreamMux.QueueSizeGapDecay
import Proofs.StreamMux.Smoothing
import Proofs.StreamMux.BitrateControl

open GapDecaySpec

set_option linter.unusedVariables false

/-! ================================================================
    INV1: Numerical Safety — non-zero denominators
    ================================================================ -/

theorem smoothing_denom_nonzero (inertiaDen : Nat) (count : Nat)
    (hDen : inertiaDen > 0) :
    smoothingDenomSafe inertiaDen count := by
  unfold smoothingDenomSafe StreamMux.smoothedDen
  exact Nat.lt_of_lt_of_le (Nat.zero_lt_of_lt hDen)
    (Nat.le_mul_of_pos_right inertiaDen (by omega))

theorem checkOnce_guards_derivative
    (s : SystemState) (inp : CheckOnceInput)
    (hBad : inp.nowMs - s.lastCheckTS ≤ 0) :
    sysCheckOnce s inp = s := by
  unfold sysCheckOnce; simp [hBad]

theorem gapDecay_division_safe (cfg : GapDecayConfig)
    (hSafe : cfg.safe) : cfg.gapDecay ≠ 0 := by
  obtain ⟨h, _, _⟩ := hSafe; omega

theorem inertia_increase_denom_safe (cfg : GapDecayConfig)
    (hSafe : cfg.safe) : cfg.inertiaIncrease ≠ 0 := by
  obtain ⟨_, h, _⟩ := hSafe; omega

theorem inertia_decrease_denom_safe (cfg : GapDecayConfig)
    (hSafe : cfg.safe) (checkInterval : Int) (hInt : checkInterval ≥ 0) :
    cfg.inertiaDecrease + checkInterval ≠ 0 := by
  obtain ⟨_, _, h⟩ := hSafe; omega

theorem safeDiv_none_when_zero (a : Int) :
    safeDiv a 0 = none := by
  unfold safeDiv; simp

theorem safeDiv_some_when_nonzero (a b : Int) (hb : b ≠ 0) :
    safeDiv a b = some (a / b) := by
  unfold safeDiv; simp [hb]

/-! ================================================================
    INV2: Data Flow Correctness
    ================================================================ -/

theorem data_flow_correct_always
    (s : SystemState) (inp : LatencyInput) :
    dataFlowCorrect s inp := by
  unfold dataFlowCorrect measureLatency; simp

theorem buggy_data_flow_wrong
    (s : SystemState) (inp : LatencyInput)
    (hDiff : sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS ≠
             sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS) :
    (measureLatencyBuggy s inp).audioMeasurements.sendingLatency ≠
    sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS := by
  unfold measureLatencyBuggy; simp; exact hDiff.symm

theorem correct_vs_buggy_audio_latency
    (s : SystemState) (inp : LatencyInput)
    (hDiff : sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS ≠
             sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS) :
    (measureLatency s inp).audioMeasurements.sendingLatency ≠
    (measureLatencyBuggy s inp).audioMeasurements.sendingLatency := by
  unfold measureLatency measureLatencyBuggy; simp; exact hDiff

theorem data_flow_after_latency_step
    (s : SystemState) (inp : LatencyInput) :
    let s' := sysStep s (SystemInput.measureLatencyEvt inp)
    s'.audioMeasurements.sendingLatency =
      sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS ∧
    s'.videoMeasurements.sendingLatency =
      sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS := by
  unfold sysStep measureLatency; simp

theorem checkOnce_preserves_latency_source
    (s : SystemState) (inp : CheckOnceInput) :
    (sysCheckOnce s inp).audioMeasurements.sendingLatency =
      s.audioMeasurements.sendingLatency ∧
    (sysCheckOnce s inp).videoMeasurements.sendingLatency =
      s.videoMeasurements.sendingLatency := by
  unfold sysCheckOnce
  simp only
  split
  · exact ⟨rfl, rfl⟩
  · split
    · exact ⟨rfl, rfl⟩
    · split
      · exact ⟨rfl, rfl⟩
      · exact ⟨rfl, rfl⟩

/-! ================================================================
    INV3: Resolution Stability
    ================================================================ -/

theorem downgrade_blocked_within_duration
    (prev : SlowdownRequest)
    (now : Int)
    (upgradeMs downgradeMs : Int)
    (targetPx avgPx : Int)
    (hDir : prev.isUpgrade = false)
    (hRecent : now - prev.latestAt ≤ 60000)
    (hEarly : now - prev.startedAt < downgradeMs) :
    checkSlowdown (some prev) false now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨false, prev.startedAt, now⟩ := by
  unfold checkSlowdown; simp [hDir]
  have : ¬(now - prev.latestAt > 60000) := by omega
  simp [this]; unfold checkSlowdownBody; simp; omega

theorem direction_reversal_resets_none
    (isUpgrade : Bool) (nowMs upgradeMs downgradeMs targetPx avgPx : Int) :
    checkSlowdown none isUpgrade nowMs upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨isUpgrade, nowMs, nowMs⟩ := by
  unfold checkSlowdown; simp

theorem direction_reversal_resets_some
    (prev : SlowdownRequest) (isUpgrade : Bool)
    (nowMs upgradeMs downgradeMs targetPx avgPx : Int)
    (hDir : prev.isUpgrade ≠ isUpgrade) :
    checkSlowdown (some prev) isUpgrade nowMs upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨isUpgrade, nowMs, nowMs⟩ := by
  unfold checkSlowdown; simp [hDir]

theorem downgrade_proceeds_after_duration
    (prev : SlowdownRequest)
    (now : Int)
    (upgradeMs downgradeMs : Int)
    (targetPx avgPx : Int)
    (hDir : prev.isUpgrade = false)
    (hRecent : now - prev.latestAt ≤ 60000)
    (hEnough : now - prev.startedAt ≥ downgradeMs) :
    checkSlowdown (some prev) false now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.proceed := by
  unfold checkSlowdown; simp [hDir]
  have : ¬(now - prev.latestAt > 60000) := by omega
  simp [this]; unfold checkSlowdownBody; simp; omega

theorem first_slowdown_call_blocks
    (isUpgrade : Bool) (nowMs upgradeMs downgradeMs targetPx avgPx : Int) :
    ∃ req, checkSlowdown none isUpgrade nowMs upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime req :=
  ⟨_, direction_reversal_resets_none _ _ _ _ _ _⟩

theorem resolution_stability_downgrade
    (prevStartedAt latestAt nowMs downgradeMs : Int)
    (hDur : nowMs - prevStartedAt < downgradeMs)
    (hRecent : nowMs - latestAt ≤ 60000)
    (upgradeMs targetPx avgPx : Int) :
    checkSlowdown
      (some ⟨false, prevStartedAt, latestAt⟩)
      false nowMs upgradeMs downgradeMs targetPx avgPx =
    SlowdownResult.notThisTime ⟨false, prevStartedAt, nowMs⟩ := by
  unfold checkSlowdown; simp
  have : ¬(nowMs - latestAt > 60000) := by omega
  simp [this]; unfold checkSlowdownBody; simp; omega

/-! ================================================================
    INV4: Derivative Bounded
    ================================================================ -/

/-- For non-negative a and positive b, Euclidean division satisfies 0 ≤ a/b and a/b*b ≤ a. -/
theorem ediv_bounded_nonneg (a b : Int) (ha : a ≥ 0) (hb : b > 0) :
    a / b * b ≤ a ∧ 0 ≤ a / b := by
  have hb_ne : b ≠ 0 := by omega
  have hmod_nn := Int.emod_nonneg a hb_ne
  have hmod_bd := Int.emod_lt_of_pos a hb
  have hdecomp := Int.ediv_add_emod a b
  constructor
  · -- a / b * b ≤ a: since a = b * (a/b) + a%b and a%b ≥ 0
    have : a / b * b = b * (a / b) := Int.mul_comm (a / b) b
    rw [this]; omega
  · exact Int.ediv_nonneg ha (by omega)

theorem derivative_bounded_nonneg
    (a b : Int) (ha : a ≥ 0) (hb : b > 0) :
    derivativeBoundedNonneg a b := by
  unfold derivativeBoundedNonneg; intro _ _
  exact ediv_bounded_nonneg a b ha hb

theorem derivative_zero_when_unchanged
    (queue tsDiffMs : Int) (hTsDiff : tsDiffMs > 0) :
    (queue - queue) * 1000 / tsDiffMs = 0 := by simp

theorem derivative_sign_matches_direction
    (newQueue oldQueue tsDiffMs : Int)
    (hTsDiff : tsDiffMs > 0) (hGrow : newQueue > oldQueue) :
    (newQueue - oldQueue) * 1000 / tsDiffMs ≥ 0 :=
  Int.ediv_nonneg (by omega) (by omega)

/-! ================================================================
    INV5: Bitrate Convergence Under Constant Conditions
    ================================================================ -/

theorem equilibrium_no_change
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hEquil : atEquilibrium cfg req) :
    computeBitRateDiff cfg req = 0 := by
  obtain ⟨hQueue, hDeriv⟩ := hEquil
  exact bitRateDiff_zero_at_optimal_zero_deriv cfg req hQueue hDeriv

theorem equilibrium_raw_bitrate
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hEquil : atEquilibrium cfg req) :
    GapDecaySpec.computeRawNewBitRate cfg req = max req.currentBitRate 1 := by
  have h := equilibrium_no_change cfg req hEquil
  -- h: computeBitRateDiff cfg req = 0
  -- computeRawNewBitRate = rawNewBitRate req.currentBitRate (computeBitRateDiff cfg req)
  --                      = max (req.currentBitRate + 0) 1 = max req.currentBitRate 1
  show GapDecaySpec.rawNewBitRate req.currentBitRate (computeBitRateDiff cfg req) =
    max req.currentBitRate 1
  rw [h]
  unfold GapDecaySpec.rawNewBitRate
  omega

theorem equilibrium_preserves_bitrate
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hEquil : atEquilibrium cfg req)
    (hPos : req.currentBitRate ≥ 1) :
    computeNewBitrate cfg req = req.currentBitRate := by
  unfold computeNewBitrate
  have hBrd : computeBitRateDiff cfg req = 0 :=
    equilibrium_no_change cfg req hEquil
  -- Simplify the let-bindings by substituting hBrd
  simp only [hBrd, GapDecaySpec.rawNewBitRate, GapDecaySpec.applyInertia]
  omega

/-! ================================================================
    INV6: Measurement Monotonicity (Convex Combination)
    ================================================================ -/

theorem measurement_value_convex
    (old new_ : Nat) (inertiaNum inertiaDen count : Nat)
    (hValid : inertiaNum * count ≤ inertiaDen * (count + 3)) :
    measurementConvex old new_ inertiaNum inertiaDen count := by
  unfold measurementConvex; intro _
  exact ⟨smoothed_ge_min old new_ inertiaNum inertiaDen count hValid,
         smoothed_le_max old new_ inertiaNum inertiaDen count hValid⟩

theorem inertia_nine_tenths_valid (count : Nat) :
    9 * count ≤ 10 * (count + 3) := by omega

theorem measurement_convex_nine_tenths
    (old new_ : Nat) (count : Nat) :
    measurementConvex old new_ 9 10 count :=
  measurement_value_convex old new_ 9 10 count (inertia_nine_tenths_valid count)

theorem measureBitRates_preserves_latency
    (s : SystemState) (inp : MeasurementInput) :
    (measureBitRates s inp).videoMeasurements.sendingLatency =
      s.videoMeasurements.sendingLatency ∧
    (measureBitRates s inp).audioMeasurements.sendingLatency =
      s.audioMeasurements.sendingLatency := by
  unfold measureBitRates; cases inp.mediaType <;> simp

theorem measureLatency_preserves_bitrates
    (s : SystemState) (inp : LatencyInput) :
    (measureLatency s inp).videoMeasurements.inputBitRate =
      s.videoMeasurements.inputBitRate ∧
    (measureLatency s inp).videoMeasurements.outputBitRate =
      s.videoMeasurements.outputBitRate ∧
    (measureLatency s inp).audioMeasurements.inputBitRate =
      s.audioMeasurements.inputBitRate := by
  unfold measureLatency; simp

/-! ================================================================
    Cross-cutting: Composability of transitions
    ================================================================ -/

theorem measureBitRates_preserves_config
    (s : SystemState) (inp : MeasurementInput) :
    (measureBitRates s inp).minBitRate = s.minBitRate ∧
    (measureBitRates s inp).maxBitRate = s.maxBitRate ∧
    (measureBitRates s inp).upgradeSlowdownMs = s.upgradeSlowdownMs ∧
    (measureBitRates s inp).downgradeSlowdownMs = s.downgradeSlowdownMs := by
  unfold measureBitRates; cases inp.mediaType <;> simp

theorem measureLatency_preserves_config
    (s : SystemState) (inp : LatencyInput) :
    (measureLatency s inp).minBitRate = s.minBitRate ∧
    (measureLatency s inp).maxBitRate = s.maxBitRate ∧
    (measureLatency s inp).upgradeSlowdownMs = s.upgradeSlowdownMs ∧
    (measureLatency s inp).downgradeSlowdownMs = s.downgradeSlowdownMs := by
  unfold measureLatency; simp

theorem checkOnce_preserves_config
    (s : SystemState) (inp : CheckOnceInput) :
    (sysCheckOnce s inp).minBitRate = s.minBitRate ∧
    (sysCheckOnce s inp).maxBitRate = s.maxBitRate ∧
    (sysCheckOnce s inp).upgradeSlowdownMs = s.upgradeSlowdownMs ∧
    (sysCheckOnce s inp).downgradeSlowdownMs = s.downgradeSlowdownMs := by
  unfold sysCheckOnce
  simp only
  split
  · exact ⟨rfl, rfl, rfl, rfl⟩
  · split
    · exact ⟨rfl, rfl, rfl, rfl⟩
    · split
      · exact ⟨rfl, rfl, rfl, rfl⟩
      · exact ⟨rfl, rfl, rfl, rfl⟩

theorem step_preserves_config
    (s : SystemState) (inp : SystemInput) :
    (sysStep s inp).minBitRate = s.minBitRate ∧
    (sysStep s inp).maxBitRate = s.maxBitRate ∧
    (sysStep s inp).upgradeSlowdownMs = s.upgradeSlowdownMs ∧
    (sysStep s inp).downgradeSlowdownMs = s.downgradeSlowdownMs := by
  cases inp with
  | measureBitRatesEvt i => exact measureBitRates_preserves_config s i
  | measureLatencyEvt i => exact measureLatency_preserves_config s i
  | checkOnceEvt i => exact checkOnce_preserves_config s i

theorem non_latency_step_preserves_latency
    (s : SystemState) (i : MeasurementInput) :
    (sysStep s (SystemInput.measureBitRatesEvt i)).videoMeasurements.sendingLatency =
      s.videoMeasurements.sendingLatency := by
  unfold sysStep; exact (measureBitRates_preserves_latency s i).1

theorem checkOnce_step_preserves_latency
    (s : SystemState) (i : CheckOnceInput) :
    (sysStep s (SystemInput.checkOnceEvt i)).videoMeasurements.sendingLatency =
      s.videoMeasurements.sendingLatency ∧
    (sysStep s (SystemInput.checkOnceEvt i)).audioMeasurements.sendingLatency =
      s.audioMeasurements.sendingLatency := by
  show (sysCheckOnce s i).videoMeasurements.sendingLatency = _ ∧
       (sysCheckOnce s i).audioMeasurements.sendingLatency = _
  have h := checkOnce_preserves_latency_source s i
  exact ⟨h.2, h.1⟩

/-! ### System-level composites -/

theorem inv2_reachable_data_flow
    (init s : SystemState) (_hReach : Reachable init s) (inp : LatencyInput) :
    dataFlowCorrect s inp :=
  data_flow_correct_always s inp

theorem inv5_equilibrium_bitrate_diff_zero
    (cfg : GapDecayConfig) (req : GapDecayRequest)
    (hQueue : req.queueDuration = GapDecaySpec.queueDurationOptimal cfg req.actualOutputBitRate)
    (hDeriv : req.actualDerivative = 0)
    (hPos : req.currentBitRate ≥ 1) :
    computeNewBitrate cfg req = req.currentBitRate :=
  equilibrium_preserves_bitrate cfg req ⟨hQueue, hDeriv⟩ hPos
