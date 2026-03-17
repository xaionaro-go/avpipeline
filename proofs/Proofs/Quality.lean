-- Proofs/Quality.lean: Correctness proofs for stream quality metrics

import Spec.Quality

open Quality

/-! ## Empty input → all metrics = 0 -/

/-- Empty input produces all-zero quality metrics. -/
theorem empty_input_zero :
    getStreamQuality [] =
      { continuityNum := 0, continuityDen := 1
        overlapNum := 0, overlapDen := 1
        frameRateNum := 0, frameRateDen := 1
        invalidDTS := 0 } := by
  rfl

/-! ## Single entry → interval = 0, so all ratios = 0 -/

/-- A single entry yields zero ratios (interval = endTS - endTS = 0). -/
theorem single_entry_zero (e : DTSAndDuration) :
    let q := getStreamQuality [e]
    q.continuityNum = 0 ∧ q.overlapNum = 0 ∧ q.frameRateNum = 0 := by
  simp only [getStreamQuality, List.getLast, List.dropLast, scanBackward,
    List.reverse_nil, List.foldl_nil]
  split
  · simp only [StreamQuality.continuityNum, StreamQuality.overlapNum,
               StreamQuality.frameRateNum]; omega
  · exact ⟨rfl, rfl, rfl⟩

/-! ## FrameRate numerator = 0 when ≤ 1 frame -/

/-- With a single entry, frameRate numerator is 0 (need > 1 frame). -/
theorem single_entry_frameRate_zero (e : DTSAndDuration) :
    (getStreamQuality [e]).frameRateNum = 0 := by
  simp only [getStreamQuality, List.getLast, List.dropLast, scanBackward,
    List.reverse_nil, List.foldl_nil]
  split
  · simp only [StreamQuality.frameRateNum]; omega
  · rfl

/-! ## Denominators > 0 when interval > 0 -/

/-- All denominators in the result are > 0. -/
theorem denominators_positive (entries : List DTSAndDuration)
    (hne : entries ≠ []) :
    let q := getStreamQuality entries
    q.continuityDen > 0 ∧ q.overlapDen > 0 ∧ q.frameRateDen > 0 := by
  match entries, hne with
  | h :: t, _ =>
    simp only [getStreamQuality]
    split
    · constructor
      · simp only [StreamQuality.continuityDen]; assumption
      constructor
      · simp only [StreamQuality.overlapDen]; assumption
      · simp only [StreamQuality.frameRateDen]; assumption
    · constructor
      · simp only [StreamQuality.continuityDen]; omega
      constructor
      · simp only [StreamQuality.overlapDen]; omega
      · simp only [StreamQuality.frameRateDen]; omega

/-! ## Overlap accumulator is non-negative -/

/-- processItem preserves non-negative overlapLength. -/
theorem processItem_overlap_nonneg (acc : LoopAcc) (item : DTSAndDuration)
    (h : acc.overlapLength ≥ 0) :
    (processItem acc item).overlapLength ≥ 0 := by
  simp only [processItem]
  split
  · exact h
  · simp only [LoopAcc.overlapLength]
    split
    · omega
    · exact h

/-- foldl processItem preserves non-negative overlapLength. -/
theorem foldl_processItem_overlap_nonneg (acc : LoopAcc) (items : List DTSAndDuration)
    (h : acc.overlapLength ≥ 0) :
    (items.foldl processItem acc).overlapLength ≥ 0 := by
  induction items generalizing acc with
  | nil => simpa [List.foldl]
  | cons x xs ih =>
    simp only [List.foldl]
    exact ih _ (processItem_overlap_nonneg acc x h)

/-- scanBackward preserves non-negative overlapLength. -/
theorem scanBackward_overlap_nonneg (acc : LoopAcc) (items : List DTSAndDuration)
    (h : acc.overlapLength ≥ 0) :
    (scanBackward acc items).overlapLength ≥ 0 := by
  simp only [scanBackward]
  exact foldl_processItem_overlap_nonneg acc items.reverse h

/-- Overlap numerator is always ≥ 0 in the result. -/
theorem overlap_nonneg (entries : List DTSAndDuration) :
    (getStreamQuality entries).overlapNum ≥ 0 := by
  match entries with
  | [] => simp [getStreamQuality]
  | h :: t =>
    simp only [getStreamQuality]
    have hscan := scanBackward_overlap_nonneg
      { prevDTS := ((h :: t).getLast (by simp)).dts
        minDTS := ((h :: t).getLast (by simp)).dts + ((h :: t).getLast (by simp)).duration
        discontinuityLength := 0
        overlapLength := (0 : Int)
        frameCount := 1
        invalidDTS := 0 }
      (h :: t).dropLast
      (by simp only [LoopAcc.overlapLength]; omega)
    split
    · simp only [StreamQuality.overlapNum]; exact hscan
    · simp only [StreamQuality.overlapNum]; omega

/-! ## Discontinuity accumulator is non-negative -/

/-- processItem preserves non-negative discontinuityLength. -/
theorem processItem_discont_nonneg (acc : LoopAcc) (item : DTSAndDuration)
    (h : acc.discontinuityLength ≥ 0) :
    (processItem acc item).discontinuityLength ≥ 0 := by
  simp only [processItem]
  split
  · exact h
  · simp only [LoopAcc.discontinuityLength]
    split
    · omega
    · exact h

/-- foldl processItem preserves non-negative discontinuityLength. -/
theorem foldl_processItem_discont_nonneg (acc : LoopAcc) (items : List DTSAndDuration)
    (h : acc.discontinuityLength ≥ 0) :
    (items.foldl processItem acc).discontinuityLength ≥ 0 := by
  induction items generalizing acc with
  | nil => simpa [List.foldl]
  | cons x xs ih =>
    simp only [List.foldl]
    exact ih _ (processItem_discont_nonneg acc x h)

/-- scanBackward preserves non-negative discontinuityLength. -/
theorem scanBackward_discont_nonneg (acc : LoopAcc) (items : List DTSAndDuration)
    (h : acc.discontinuityLength ≥ 0) :
    (scanBackward acc items).discontinuityLength ≥ 0 := by
  simp only [scanBackward]
  exact foldl_processItem_discont_nonneg acc items.reverse h

/-! ## processItem: InvalidDTS counts items where dts ≥ prevDTS -/

/-- processItem increments invalidDTS by 1 when item.dts ≥ acc.prevDTS. -/
theorem processItem_invalid_when_ge (acc : LoopAcc) (item : DTSAndDuration)
    (h : item.dts ≥ acc.prevDTS) :
    (processItem acc item).invalidDTS = acc.invalidDTS + 1 := by
  simp only [processItem, show item.dts >= acc.prevDTS from h, ite_true,
             LoopAcc.invalidDTS]

/-- processItem keeps invalidDTS unchanged when item.dts < acc.prevDTS. -/
theorem processItem_valid_when_lt (acc : LoopAcc) (item : DTSAndDuration)
    (h : item.dts < acc.prevDTS) :
    (processItem acc item).invalidDTS = acc.invalidDTS := by
  simp only [processItem, show ¬(item.dts ≥ acc.prevDTS) from by omega, ite_false,
             LoopAcc.invalidDTS]

/-! ## InvalidDTS monotonically increases through scanBackward -/

/-- processItem never decreases invalidDTS. -/
theorem processItem_invalidDTS_mono (acc : LoopAcc) (item : DTSAndDuration) :
    (processItem acc item).invalidDTS ≥ acc.invalidDTS := by
  simp only [processItem]
  split
  · simp only [LoopAcc.invalidDTS]; omega
  · simp only [LoopAcc.invalidDTS]; omega

/-- foldl processItem never decreases invalidDTS. -/
theorem foldl_processItem_invalidDTS_mono (acc : LoopAcc) (items : List DTSAndDuration) :
    (items.foldl processItem acc).invalidDTS ≥ acc.invalidDTS := by
  induction items generalizing acc with
  | nil => simp [List.foldl]
  | cons x xs ih =>
    simp only [List.foldl]
    have h1 := processItem_invalidDTS_mono acc x
    have h2 := ih (processItem acc x)
    omega

/-- scanBackward never decreases invalidDTS. -/
theorem scanBackward_invalidDTS_mono (acc : LoopAcc) (items : List DTSAndDuration) :
    (scanBackward acc items).invalidDTS ≥ acc.invalidDTS := by
  simp only [scanBackward]
  exact foldl_processItem_invalidDTS_mono acc items.reverse

/-! ## frameCount monotonically increases through scanBackward -/

/-- processItem never decreases frameCount. -/
theorem processItem_frameCount_mono (acc : LoopAcc) (item : DTSAndDuration) :
    (processItem acc item).frameCount ≥ acc.frameCount := by
  simp only [processItem]
  split
  · simp only [LoopAcc.frameCount]; omega
  · simp only [LoopAcc.frameCount]; omega

/-- foldl processItem never decreases frameCount. -/
theorem foldl_processItem_frameCount_mono (acc : LoopAcc) (items : List DTSAndDuration) :
    (items.foldl processItem acc).frameCount ≥ acc.frameCount := by
  induction items generalizing acc with
  | nil => simp [List.foldl]
  | cons x xs ih =>
    simp only [List.foldl]
    have h1 := processItem_frameCount_mono acc x
    have h2 := ih (processItem acc x)
    omega

/-- scanBackward never decreases frameCount. -/
theorem scanBackward_frameCount_mono (acc : LoopAcc) (items : List DTSAndDuration) :
    (scanBackward acc items).frameCount ≥ acc.frameCount := by
  simp only [scanBackward]
  exact foldl_processItem_frameCount_mono acc items.reverse

/-! ## Empty scanBackward is identity -/

/-- Scanning an empty list returns the accumulator unchanged. -/
theorem scanBackward_nil (acc : LoopAcc) :
    scanBackward acc [] = acc := by
  rfl

/-! ## Contiguous stream: processItem yields zero discontinuity for contiguous pair -/

/-- When consecutive entries are contiguous and DTS is strictly increasing,
    processItem adds nothing to discontinuity or overlap. -/
theorem processItem_contiguous_no_discont (acc : LoopAcc) (item : DTSAndDuration)
    (hlt : item.dts < acc.prevDTS)
    (hcontig : item.dts + item.duration = acc.prevDTS) :
    (processItem acc item).discontinuityLength = acc.discontinuityLength ∧
    (processItem acc item).overlapLength = acc.overlapLength := by
  simp only [processItem, show ¬(item.dts ≥ acc.prevDTS) from by omega, ite_false]
  simp only [LoopAcc.discontinuityLength, LoopAcc.overlapLength, hcontig]
  constructor
  · split <;> omega
  · split <;> omega

/-! ## Contiguous two-element stream: full continuity, zero overlap -/

/-- For two contiguous entries with strictly increasing DTS, discontinuity = 0
    and continuity numerator = denominator. -/
theorem two_contiguous_full_continuity (a b : DTSAndDuration)
    (hlt : a.dts < b.dts)
    (hcontig : a.dts + a.duration = b.dts)
    (hint : b.dts + b.duration - a.dts > 0) :
    let q := getStreamQuality [a, b]
    q.continuityNum = q.continuityDen ∧ q.overlapNum = 0 := by
  simp only [getStreamQuality, List.getLast, List.dropLast, scanBackward,
    List.reverse_cons, List.reverse_nil, List.nil_append, List.foldl_cons,
    List.foldl_nil, processItem]
  simp only [show ¬(a.dts ≥ b.dts) from by omega, ite_false]
  simp only [LoopAcc.discontinuityLength, LoopAcc.overlapLength,
             LoopAcc.minDTS, LoopAcc.prevDTS, LoopAcc.frameCount,
             LoopAcc.invalidDTS, hcontig]
  split
  · simp only [StreamQuality.continuityNum, StreamQuality.continuityDen,
               StreamQuality.overlapNum]
    constructor <;> omega
  · omega
