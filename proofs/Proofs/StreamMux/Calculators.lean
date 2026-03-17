-- Proofs/StreamMux/Calculators.lean: Correctness proofs for the three auto-bitrate calculators

import Spec.StreamMux.Calculators

open StaticCalc ThresholdsCalc LogKCalc

/-! # Static Calculator Proofs -/

/-! ## Output equals configured bitrate regardless of input -/

theorem static_output_eq_config (cfg : StaticCalc.Config) (req : CalculateBitRateRequest) :
    (StaticCalc.calculate cfg req).bitRate = cfg.bitrate := by
  simp [StaticCalc.calculate]

theorem static_output_independent_of_request (cfg : StaticCalc.Config)
    (req1 req2 : CalculateBitRateRequest) :
    (StaticCalc.calculate cfg req1).bitRate = (StaticCalc.calculate cfg req2).bitRate := by
  simp [StaticCalc.calculate]

/-! ## Static always sets isCritical to true (matching Go code) -/

theorem static_always_critical (cfg : StaticCalc.Config) (req : CalculateBitRateRequest) :
    (StaticCalc.calculate cfg req).isCritical = true := by
  simp [StaticCalc.calculate]

/-! ## Static is fully determined by config alone -/

theorem static_deterministic (cfg : StaticCalc.Config) (req1 req2 : CalculateBitRateRequest) :
    StaticCalc.calculate cfg req1 = StaticCalc.calculate cfg req2 := by
  simp [StaticCalc.calculate]

/-! # Thresholds Calculator Proofs -/

/-! ## Zone classification correctness -/

theorem classify_extremely_high (cfg : ThresholdsCalc.Config) (qd : Int)
    (h : qd ≥ cfg.extremelyHighDuration) :
    ThresholdsCalc.classify cfg qd = Zone.extremelyHigh := by
  unfold ThresholdsCalc.classify
  simp [h]

theorem classify_very_high (cfg : ThresholdsCalc.Config) (qd : Int)
    (h1 : qd ≥ cfg.veryHighDuration) (h2 : qd < cfg.extremelyHighDuration) :
    ThresholdsCalc.classify cfg qd = Zone.veryHigh := by
  unfold ThresholdsCalc.classify
  have : ¬(qd ≥ cfg.extremelyHighDuration) := by omega
  simp [this, h1]

theorem classify_very_low (cfg : ThresholdsCalc.Config) (qd : Int)
    (h1 : qd ≤ cfg.veryLowDuration)
    (h2 : qd < cfg.extremelyHighDuration)
    (h3 : qd < cfg.veryHighDuration) :
    ThresholdsCalc.classify cfg qd = Zone.veryLow := by
  unfold ThresholdsCalc.classify
  have hne : ¬(qd ≥ cfg.extremelyHighDuration) := by omega
  have hnv : ¬(qd ≥ cfg.veryHighDuration) := by omega
  simp [hne, hnv, h1]

theorem classify_high (cfg : ThresholdsCalc.Config) (qd : Int)
    (h1 : qd ≥ cfg.highDuration) (h2 : qd < cfg.veryHighDuration)
    (h3 : qd < cfg.extremelyHighDuration) (h4 : qd > cfg.veryLowDuration) :
    ThresholdsCalc.classify cfg qd = Zone.high := by
  unfold ThresholdsCalc.classify
  have hne : ¬(qd ≥ cfg.extremelyHighDuration) := by omega
  have hnv : ¬(qd ≥ cfg.veryHighDuration) := by omega
  have hnvl : ¬(qd ≤ cfg.veryLowDuration) := by omega
  simp [hne, hnv, hnvl, h1]

theorem classify_low (cfg : ThresholdsCalc.Config) (qd : Int)
    (h1 : qd ≤ cfg.lowDuration) (h2 : qd > cfg.veryLowDuration)
    (h3 : qd < cfg.highDuration) (h4 : qd < cfg.veryHighDuration)
    (h5 : qd < cfg.extremelyHighDuration) :
    ThresholdsCalc.classify cfg qd = Zone.low := by
  unfold ThresholdsCalc.classify
  have hne : ¬(qd ≥ cfg.extremelyHighDuration) := by omega
  have hnv : ¬(qd ≥ cfg.veryHighDuration) := by omega
  have hnvl : ¬(qd ≤ cfg.veryLowDuration) := by omega
  have hnh : ¬(qd ≥ cfg.highDuration) := by omega
  simp [hne, hnv, hnvl, hnh, h1]

theorem classify_normal (cfg : ThresholdsCalc.Config) (qd : Int)
    (h1 : qd > cfg.lowDuration) (h2 : qd < cfg.highDuration)
    (h3 : qd > cfg.veryLowDuration)
    (h4 : qd < cfg.veryHighDuration) (h5 : qd < cfg.extremelyHighDuration) :
    ThresholdsCalc.classify cfg qd = Zone.normal := by
  unfold ThresholdsCalc.classify
  have hne : ¬(qd ≥ cfg.extremelyHighDuration) := by omega
  have hnv : ¬(qd ≥ cfg.veryHighDuration) := by omega
  have hnvl : ¬(qd ≤ cfg.veryLowDuration) := by omega
  have hnh : ¬(qd ≥ cfg.highDuration) := by omega
  have hnl : ¬(qd ≤ cfg.lowDuration) := by omega
  simp [hne, hnv, hnvl, hnh, hnl]

/-! ## High queue (decrease zones) → bitrate decreases -/

theorem thresholds_high_queue_decreases (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.high)
    (hPos : req.currentBitrateSetting > 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate < req.currentBitrateSetting := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.decreaseNum ≠ cfg.decreaseDen := by
    have := hWf.dec_lt; omega
  simp [hne]
  have hdenpos := hWf.dens_pos.2.2.1
  have hlt := hWf.dec_lt
  exact Int.ediv_lt_of_lt_mul hdenpos
    (Int.mul_lt_mul_of_pos_left hlt hPos)

theorem thresholds_very_high_queue_decreases (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.veryHigh)
    (hPos : req.currentBitrateSetting > 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate < req.currentBitrateSetting := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.quickDecreaseNum ≠ cfg.quickDecreaseDen := by
    have := hWf.quick_dec_lt; omega
  simp [hne]
  have hdenpos := hWf.dens_pos.2.1
  have hlt := hWf.quick_dec_lt
  exact Int.ediv_lt_of_lt_mul hdenpos
    (Int.mul_lt_mul_of_pos_left hlt hPos)

theorem thresholds_extremely_high_queue_decreases (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.extremelyHigh)
    (hPos : req.currentBitrateSetting > 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate < req.currentBitrateSetting := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.extremeDecreaseNum ≠ cfg.extremeDecreaseDen := by
    have := hWf.extreme_lt; omega
  simp [hne]
  have hdenpos := hWf.dens_pos.1
  have hlt := hWf.extreme_lt
  exact Int.ediv_lt_of_lt_mul hdenpos
    (Int.mul_lt_mul_of_pos_left hlt hPos)

/-! ## Low queue (increase zones) → bitrate increases -/

theorem thresholds_low_queue_increases (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.low)
    (hPos : req.currentBitrateSetting > 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate ≥ req.currentBitrateSetting := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.increaseNum ≠ cfg.increaseDen := by
    have := hWf.inc_gt; omega
  simp [hne]
  have hdenpos := hWf.dens_pos.2.2.2.1
  have hgt := hWf.inc_gt
  exact Int.le_ediv_of_mul_le hdenpos
    (Int.mul_le_mul_of_nonneg_left (Int.le_of_lt hgt) (Int.le_of_lt hPos))

theorem thresholds_very_low_queue_increases (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.veryLow)
    (hPos : req.currentBitrateSetting > 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate ≥ req.currentBitrateSetting := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.quickIncreaseNum ≠ cfg.quickIncreaseDen := by
    have := hWf.quick_inc_gt; omega
  simp [hne]
  have hdenpos := hWf.dens_pos.2.2.2.2
  have hgt := hWf.quick_inc_gt
  exact Int.le_ediv_of_mul_le hdenpos
    (Int.mul_le_mul_of_nonneg_left (Int.le_of_lt hgt) (Int.le_of_lt hPos))

/-! ## Normal zone → no change -/

theorem thresholds_normal_unchanged (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.normal) :
    (ThresholdsCalc.calculate cfg req qd).bitRate = req.currentBitrateSetting := by
  simp [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]

theorem thresholds_normal_not_critical (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.normal) :
    (ThresholdsCalc.calculate cfg req qd).isCritical = false := by
  simp [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]

/-! ## Critical only in extreme/veryHigh zones -/

theorem thresholds_high_not_critical (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.high) :
    (ThresholdsCalc.calculate cfg req qd).isCritical = false := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.decreaseNum ≠ cfg.decreaseDen := by
    have := hWf.dec_lt; omega
  simp [hne]

theorem thresholds_low_not_critical (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.low) :
    (ThresholdsCalc.calculate cfg req qd).isCritical = false := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.increaseNum ≠ cfg.increaseDen := by
    have := hWf.inc_gt; omega
  simp [hne]

theorem thresholds_extremely_high_critical (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.extremelyHigh) :
    (ThresholdsCalc.calculate cfg req qd).isCritical = true := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.extremeDecreaseNum ≠ cfg.extremeDecreaseDen := by
    have := hWf.extreme_lt; omega
  simp [hne]

theorem thresholds_very_high_critical (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hZone : ThresholdsCalc.classify cfg qd = Zone.veryHigh) :
    (ThresholdsCalc.calculate cfg req qd).isCritical = true := by
  simp only [ThresholdsCalc.calculate, hZone, ThresholdsCalc.zoneMultiplier]
  have hne : cfg.quickDecreaseNum ≠ cfg.quickDecreaseDen := by
    have := hWf.quick_dec_lt; omega
  simp [hne]

/-! ## Output non-negative when input is non-negative -/

private theorem ediv_nonneg_of_mul_nonneg (a b c : Int) (ha : a ≥ 0) (hb : b > 0) (hc : c > 0) :
    a * b / c ≥ 0 :=
  Int.ediv_nonneg (Int.mul_nonneg ha (Int.le_of_lt hb)) (Int.le_of_lt hc)

theorem thresholds_output_nonneg (cfg : ThresholdsCalc.Config)
    (req : CalculateBitRateRequest) (qd : Int)
    (hWf : ThresholdsCalc.ConfigWF cfg)
    (hPos : req.currentBitrateSetting ≥ 0) :
    (ThresholdsCalc.calculate cfg req qd).bitRate ≥ 0 := by
  simp only [ThresholdsCalc.calculate]
  cases hz : ThresholdsCalc.classify cfg qd <;>
    simp only [ThresholdsCalc.zoneMultiplier, hz]
  -- extremelyHigh
  · have hne : cfg.extremeDecreaseNum ≠ cfg.extremeDecreaseDen := by
      have := hWf.extreme_lt; omega
    simp [hne]
    exact ediv_nonneg_of_mul_nonneg _ _ _ hPos hWf.decrease_nums_pos.1 hWf.dens_pos.1
  -- veryHigh
  · have hne : cfg.quickDecreaseNum ≠ cfg.quickDecreaseDen := by
      have := hWf.quick_dec_lt; omega
    simp [hne]
    exact ediv_nonneg_of_mul_nonneg _ _ _ hPos hWf.decrease_nums_pos.2.1 hWf.dens_pos.2.1
  -- high
  · have hne : cfg.decreaseNum ≠ cfg.decreaseDen := by
      have := hWf.dec_lt; omega
    simp [hne]
    exact ediv_nonneg_of_mul_nonneg _ _ _ hPos hWf.decrease_nums_pos.2.2.1 hWf.dens_pos.2.2.1
  -- normal
  · simp; omega
  -- low
  · have hne : cfg.increaseNum ≠ cfg.increaseDen := by
      have := hWf.inc_gt; omega
    simp [hne]
    exact ediv_nonneg_of_mul_nonneg _ _ _ hPos hWf.decrease_nums_pos.2.2.2.1 hWf.dens_pos.2.2.2.1
  -- veryLow
  · have hne : cfg.quickIncreaseNum ≠ cfg.quickIncreaseDen := by
      have := hWf.quick_inc_gt; omega
    simp [hne]
    exact ediv_nonneg_of_mul_nonneg _ _ _ hPos hWf.decrease_nums_pos.2.2.2.2 hWf.dens_pos.2.2.2.2

/-! # LogK Calculator Proofs -/

/-! ## kRatio monotonicity: larger queue → smaller numerator/denominator ratio -/

/-- When queue duration increases, k's denominator increases while numerator stays fixed. -/
theorem logk_k_denom_monotone (cfg : LogKCalc.Config) (qd1 qd2 : Int)
    (h : qd1 ≤ qd2) :
    (LogKCalc.kRatio cfg qd1).2 ≤ (LogKCalc.kRatio cfg qd2).2 := by
  simp [LogKCalc.kRatio]; omega

/-- The numerator of kRatio is independent of queue duration. -/
theorem logk_k_num_constant (cfg : LogKCalc.Config) (qd1 qd2 : Int) :
    (LogKCalc.kRatio cfg qd1).1 = (LogKCalc.kRatio cfg qd2).1 := by
  simp [LogKCalc.kRatio]

/-- When queue equals optimal, kRatio numerator = denominator (k = 1). -/
theorem logk_k_unity_at_optimal (cfg : LogKCalc.Config) :
    (LogKCalc.kRatio cfg cfg.queueOptimal).1 =
    (LogKCalc.kRatio cfg cfg.queueOptimal).2 := by
  simp [LogKCalc.kRatio]

/-! ## signOfChange tracks queue-vs-optimal comparison -/

theorem logk_sign_positive_when_below_optimal (kNum kDen : Int) (h : kNum > kDen) :
    LogKCalc.signOfChange kNum kDen = 1 := by
  simp [LogKCalc.signOfChange, h]

theorem logk_sign_negative_when_above_optimal (kNum kDen : Int) (h : kNum < kDen) :
    LogKCalc.signOfChange kNum kDen = -1 := by
  simp [LogKCalc.signOfChange]; omega

theorem logk_sign_zero_at_optimal (kNum kDen : Int) (h : kNum = kDen) :
    LogKCalc.signOfChange kNum kDen = 0 := by
  simp [LogKCalc.signOfChange]; omega

/-! ## Monotonicity: larger queue gap → sign goes from positive to zero/negative -/

theorem logk_monotone_sign (cfg : LogKCalc.Config) (qd1 qd2 : Int)
    (hLt : qd1 < qd2)
    (hAbove : qd1 > cfg.queueOptimal) :
    LogKCalc.signOfChange (LogKCalc.kRatio cfg qd1).1 (LogKCalc.kRatio cfg qd1).2 ≤
    LogKCalc.signOfChange (LogKCalc.kRatio cfg qd2).1 (LogKCalc.kRatio cfg qd2).2 := by
  simp [LogKCalc.kRatio, LogKCalc.signOfChange]
  omega

theorem logk_below_optimal_positive (cfg : LogKCalc.Config) (qd : Int)
    (hBelow : qd < cfg.queueOptimal) :
    LogKCalc.signOfChange (LogKCalc.kRatio cfg qd).1 (LogKCalc.kRatio cfg qd).2 = 1 := by
  simp [LogKCalc.kRatio, LogKCalc.signOfChange]
  omega

theorem logk_above_optimal_negative (cfg : LogKCalc.Config) (qd : Int)
    (hAbove : qd > cfg.queueOptimal) :
    LogKCalc.signOfChange (LogKCalc.kRatio cfg qd).1 (LogKCalc.kRatio cfg qd).2 = -1 := by
  simp [LogKCalc.kRatio, LogKCalc.signOfChange]
  omega

theorem logk_at_optimal_zero (cfg : LogKCalc.Config) :
    LogKCalc.signOfChange (LogKCalc.kRatio cfg cfg.queueOptimal).1
      (LogKCalc.kRatio cfg cfg.queueOptimal).2 = 0 := by
  simp [LogKCalc.kRatio, LogKCalc.signOfChange]

/-! ## clampMin guarantees output >= 1 -/

theorem logk_clamp_min_ge_one (v : Int) :
    LogKCalc.clampMin v ≥ 1 := by
  simp [LogKCalc.clampMin]; omega

theorem logk_clamp_min_preserves_large (v : Int) (h : v ≥ 1) :
    LogKCalc.clampMin v = v := by
  simp [LogKCalc.clampMin]; omega

theorem logk_clamp_min_floors_small (v : Int) (h : v < 1) :
    LogKCalc.clampMin v = 1 := by
  simp [LogKCalc.clampMin]; omega

/-! ## kRatio positivity: both components positive with well-formed config -/

theorem logk_k_num_pos (cfg : LogKCalc.Config) (hWf : LogKCalc.ConfigWF cfg) (qd : Int) :
    (LogKCalc.kRatio cfg qd).1 > 0 := by
  simp [LogKCalc.kRatio]
  have := hWf.opt_pos
  have := hWf.err_pos
  omega

theorem logk_k_den_pos (cfg : LogKCalc.Config) (hWf : LogKCalc.ConfigWF cfg)
    (qd : Int) (hQd : qd ≥ 0) :
    (LogKCalc.kRatio cfg qd).2 > 0 := by
  simp [LogKCalc.kRatio]
  have := hWf.err_pos
  omega
