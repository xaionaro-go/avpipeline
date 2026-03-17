-- Proofs/StreamMux/BitrateControl.lean: Correctness proofs for
-- Resolution Selection, Bitrate Clamping/Slowdown, and Temporary FPS Reduction.

import Spec.StreamMux.BitrateControl

/-! ## 1. Resolution Selection Proofs -/

/-! ### Bitrate in [Low, High] → no change -/

theorem resolution_in_range_no_change
    (allowed all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int)
    (cur : ResConfig)
    (hFind : all.find curW curH = some cur)
    (hAllowed : (allowed.find curW curH).isSome = true)
    (hLow : bitrate >= cur.bitrateLow)
    (hHigh : bitrate <= cur.bitrateHigh) :
    changeResolutionIfNeeded allowed all curW curH bitrate = ResolutionAction.noChange := by
  unfold changeResolutionIfNeeded
  simp [hFind, hAllowed]
  omega

/-! ### Bitrate < Low → selects lower resolution (switchTo or noChange at lowest) -/

theorem resolution_below_low_selects_lower
    (allowed all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int)
    (cur : ResConfig)
    (hFind : all.find curW curH = some cur)
    (hAllowed : (allowed.find curW curH).isSome = true)
    (hBelow : bitrate < cur.bitrateLow)
    (hAbove : bitrate ≤ cur.bitrateHigh) :
    let result := changeResolutionIfNeeded allowed all curW curH bitrate
    result = ResolutionAction.noChange ∨
    (∃ d, result = ResolutionAction.switchTo d) := by
  unfold changeResolutionIfNeeded
  simp [hFind, hAllowed]
  have hNotInRange : ¬(cur.bitrateLow ≤ bitrate ∧ bitrate ≤ cur.bitrateHigh) := by omega
  simp only [ge_iff_le, hNotInRange, ↓reduceIte]
  have hNotGt : ¬(bitrate > cur.bitrateHigh) := by omega
  match hd : getDesiredResolutionConfig allowed all curW curH bitrate with
  | some d =>
    simp
    by_cases hSame : d.sameRes cur
    · left; simp [hSame, hNotGt]
    · right; exact ⟨d, by simp [hSame]⟩
  | none =>
    left; simp [hNotGt]

/-! ### Bitrate > High at max resolution → enables bypass -/

theorem resolution_above_high_at_max_enables_bypass
    (allowed all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int)
    (cur : ResConfig)
    (hFind : all.find curW curH = some cur)
    (hAllowed : (allowed.find curW curH).isSome = true)
    (hAbove : bitrate > cur.bitrateHigh)
    (hDesiredSame : getDesiredResolutionConfig allowed all curW curH bitrate = some cur)
    (hSameRes : cur.sameRes cur = true) :
    changeResolutionIfNeeded allowed all curW curH bitrate = ResolutionAction.enableBypass := by
  unfold changeResolutionIfNeeded
  simp [hFind, hAllowed]
  have hNotInRange : ¬(cur.bitrateLow ≤ bitrate ∧ bitrate ≤ cur.bitrateHigh) := by omega
  simp [hNotInRange, hDesiredSame, hSameRes, hAbove]

/-! ### Current resolution not in allowed set → forces switch -/

theorem resolution_not_allowed_forces_switch
    (allowed all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int)
    (cur desired : ResConfig)
    (hFind : all.find curW curH = some cur)
    (hNotAllowed : (allowed.find curW curH).isNone = true)
    (hDesired : getDesiredResolutionConfig allowed all curW curH bitrate = some desired)
    (hDiff : desired.sameRes cur = false) :
    changeResolutionIfNeeded allowed all curW curH bitrate = ResolutionAction.switchTo desired := by
  unfold changeResolutionIfNeeded
  have hNotSome : (allowed.find curW curH).isSome = false := by
    cases h : allowed.find curW curH <;> simp_all
  simp [hFind, hNotSome, hDesired, hDiff]

/-! ## 2. Bitrate Clamping Proofs -/

/-! ### Clamped value is always ≥ minBitRate (given min ≤ effectiveMax) -/

theorem clamp_ge_min (minBitRate maxBitRate inputBitRate bitrate : Int)
    (hMinLeMax : minBitRate ≤ effectiveMaxBitRate minBitRate maxBitRate inputBitRate) :
    clampBitRate minBitRate maxBitRate inputBitRate bitrate ≥ minBitRate := by
  unfold clampBitRate
  by_cases hLow : bitrate < minBitRate
  · simp [hLow]
  · simp [hLow]
    by_cases hHigh : bitrate > effectiveMaxBitRate minBitRate maxBitRate inputBitRate
    · simp [hHigh]; exact hMinLeMax
    · simp [hHigh]; omega

/-! ### Clamped value is always ≤ effectiveMax -/

theorem clamp_le_max (minBitRate maxBitRate inputBitRate bitrate : Int)
    (hMinLeMax : minBitRate ≤ effectiveMaxBitRate minBitRate maxBitRate inputBitRate) :
    clampBitRate minBitRate maxBitRate inputBitRate bitrate ≤
      effectiveMaxBitRate minBitRate maxBitRate inputBitRate := by
  unfold clampBitRate
  by_cases hLow : bitrate < minBitRate
  · simp [hLow]; exact hMinLeMax
  · simp [hLow]
    by_cases hHigh : bitrate > effectiveMaxBitRate minBitRate maxBitRate inputBitRate
    · simp [hHigh]
    · simp [hHigh]; omega

/-! ### Combined: clamped ∈ [minBitRate, effectiveMax] -/

theorem clamp_in_range (minBitRate maxBitRate inputBitRate bitrate : Int)
    (hMinLeMax : minBitRate ≤ effectiveMaxBitRate minBitRate maxBitRate inputBitRate) :
    let c := clampBitRate minBitRate maxBitRate inputBitRate bitrate
    c ≥ minBitRate ∧ c ≤ effectiveMaxBitRate minBitRate maxBitRate inputBitRate := by
  constructor
  · exact clamp_ge_min minBitRate maxBitRate inputBitRate bitrate hMinLeMax
  · exact clamp_le_max minBitRate maxBitRate inputBitRate bitrate hMinLeMax

/-! ### effectiveMax characterization -/

theorem effectiveMax_eq_maxBitRate_when_cond_false
    (minBitRate maxBitRate inputBitRate : Int)
    (hCond : ¬(inputBitRate > minBitRate * 2 ∧ 3 * inputBitRate < 2 * maxBitRate)) :
    effectiveMaxBitRate minBitRate maxBitRate inputBitRate = maxBitRate := by
  unfold effectiveMaxBitRate
  simp [hCond]

theorem effectiveMax_eq_three_halves_when_cond_true
    (minBitRate maxBitRate inputBitRate : Int)
    (hCond : inputBitRate > minBitRate * 2 ∧ 3 * inputBitRate < 2 * maxBitRate) :
    effectiveMaxBitRate minBitRate maxBitRate inputBitRate = 3 * inputBitRate / 2 := by
  unfold effectiveMaxBitRate
  simp [hCond]

/-! ### Bypass activation/deactivation -/

theorem bypass_enabled_iff (bitrate inputBitRate : Int) :
    shouldEnableBypass bitrate inputBitRate = true ↔ 5 * bitrate > 6 * inputBitRate := by
  unfold shouldEnableBypass
  simp [decide_eq_true_eq]

theorem bypass_disabled_iff (bitrate inputBitRate : Int) :
    shouldDisableBypass bitrate inputBitRate = true ↔ bitrate < inputBitRate := by
  unfold shouldDisableBypass
  simp [decide_eq_true_eq]

/-! ## 3. Slowdown Proofs -/

/-! ### Direction change resets counter -/

theorem slowdown_direction_change_resets
    (prev : SlowdownRequest)
    (isUpgrade : Bool)
    (now : Int)
    (upgradeMs downgradeMs targetPx avgPx : Int)
    (hDir : prev.isUpgrade != isUpgrade) :
    checkSlowdown (some prev) isUpgrade now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨isUpgrade, now, now⟩ := by
  unfold checkSlowdown
  simp [hDir]

theorem slowdown_none_resets
    (isUpgrade : Bool)
    (now : Int)
    (upgradeMs downgradeMs targetPx avgPx : Int) :
    checkSlowdown none isUpgrade now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨isUpgrade, now, now⟩ := by
  unfold checkSlowdown
  simp

theorem slowdown_stale_resets
    (prev : SlowdownRequest)
    (isUpgrade : Bool)
    (now : Int)
    (upgradeMs downgradeMs targetPx avgPx : Int)
    (hSameDir : prev.isUpgrade = isUpgrade)
    (hStale : now - prev.latestAt > 60000) :
    checkSlowdown (some prev) isUpgrade now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨isUpgrade, now, now⟩ := by
  unfold checkSlowdown
  simp [hSameDir]
  omega

/-! ### Upgrade duration scales with pixel ratio -/

theorem slowdown_upgrade_scales_with_pixels
    (prev : SlowdownRequest)
    (now : Int)
    (upgradeMs downgradeMs : Int)
    (targetPx avgPx : Int)
    (hSameDir : prev.isUpgrade = true)
    (hRecent : now - prev.latestAt ≤ 60000)
    (hTargetLarger : targetPx > avgPx)
    (hAvgPos : avgPx > 0)
    (hNotEnough : now - prev.startedAt < upgradeMs * targetPx / avgPx) :
    checkSlowdown (some prev) true now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨true, prev.startedAt, now⟩ := by
  unfold checkSlowdown
  simp [hSameDir]
  have hNotStale : ¬(now - prev.latestAt > 60000) := by omega
  simp [hNotStale]
  unfold checkSlowdownBody
  simp [hTargetLarger, hAvgPos]
  omega

/-! ### Downgrade slowdown is fixed duration -/

theorem slowdown_downgrade_fixed_duration
    (prev : SlowdownRequest)
    (now : Int)
    (upgradeMs downgradeMs : Int)
    (targetPx avgPx : Int)
    (hSameDir : prev.isUpgrade = false)
    (hRecent : now - prev.latestAt ≤ 60000)
    (hNotEnough : now - prev.startedAt < downgradeMs) :
    checkSlowdown (some prev) false now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.notThisTime ⟨false, prev.startedAt, now⟩ := by
  unfold checkSlowdown
  simp [hSameDir]
  have hNotStale : ¬(now - prev.latestAt > 60000) := by omega
  simp [hNotStale]
  unfold checkSlowdownBody
  simp
  omega

theorem slowdown_downgrade_proceeds_after_duration
    (prev : SlowdownRequest)
    (now : Int)
    (upgradeMs downgradeMs : Int)
    (targetPx avgPx : Int)
    (hSameDir : prev.isUpgrade = false)
    (hRecent : now - prev.latestAt ≤ 60000)
    (hEnough : now - prev.startedAt ≥ downgradeMs) :
    checkSlowdown (some prev) false now upgradeMs downgradeMs targetPx avgPx =
      SlowdownResult.proceed := by
  unfold checkSlowdown
  simp [hSameDir]
  have hNotStale : ¬(now - prev.latestAt > 60000) := by omega
  simp [hNotStale]
  unfold checkSlowdownBody
  simp
  omega

/-! ## 4. Temporary FPS Reduction Proofs -/

/-! ### Rate-limited: returns none when < 1s since last update -/

theorem fps_reduction_rate_limited
    (st : FPSReductionState)
    (bitrate bbt curBR : Int)
    (nowMs : Int)
    (hRecent : nowMs - st.updatedAtMs < 1000) :
    temporaryReduceFPS st bitrate bbt curBR nowMs = none := by
  unfold temporaryReduceFPS
  simp [show nowMs - st.updatedAtMs < 1000 from hRecent]

/-! ### When not rate-limited, produces a new state -/

theorem fps_reduction_produces_state
    (st : FPSReductionState)
    (bitrate bbt curBR : Int)
    (nowMs : Int)
    (hNotRecent : nowMs - st.updatedAtMs ≥ 1000) :
    (temporaryReduceFPS st bitrate bbt curBR nowMs).isSome = true := by
  unfold temporaryReduceFPS
  simp [show ¬(nowMs - st.updatedAtMs < 1000) from by omega]

/-! ### The new state's timestamp is nowMs -/

theorem fps_reduction_updates_timestamp
    (st : FPSReductionState)
    (bitrate bbt curBR : Int)
    (nowMs : Int)
    (hNotRecent : nowMs - st.updatedAtMs ≥ 1000) :
    ∃ s, temporaryReduceFPS st bitrate bbt curBR nowMs = some s ∧ s.updatedAtMs = nowMs := by
  unfold temporaryReduceFPS
  have hCond : ¬(nowMs - st.updatedAtMs < 1000) := by omega
  simp [hCond]

/-! ### Multiplier = average of two sources (structural) -/

theorem fps_reduction_multiplier_is_average
    (st : FPSReductionState)
    (bitrate bbt curBR : Int)
    (nowMs : Int)
    (hNotRecent : nowMs - st.updatedAtMs ≥ 1000) :
    ∃ s, temporaryReduceFPS st bitrate bbt curBR nowMs = some s ∧
      s.multiplierNum = st.multiplierNum * bitrate * (bitrate - bbt) +
                         bitrate * (st.multiplierDen * curBR) ∧
      s.multiplierDen = 2 * (st.multiplierDen * curBR) * (bitrate - bbt) := by
  unfold temporaryReduceFPS
  have hCond : ¬(nowMs - st.updatedAtMs < 1000) := by omega
  simp [hCond]
