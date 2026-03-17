-- Proofs/StreamMux/Validation.lean: Correctness proofs for five StreamMux components.

import Spec.StreamMux.Validation

open Validation

/-! ## 1. SenderKey Comparison Proofs -/

/-- Reflexivity: comparing a SenderKey to itself yields 0. -/
theorem senderKey_cmp_refl (a : SenderKey) :
    SenderKey.cmp a a = 0 := by
  simp [SenderKey.cmp, SenderKey.compareAudio]

/-- Copy codec is preferred: if a has video copy and b does not,
    then cmp(a, b) = 1. -/
theorem senderKey_copy_preferred
    (a b : SenderKey)
    (hACopy : a.videoCodecIsCopy = true)
    (hBNotCopy : b.videoCodecIsCopy = false) :
    SenderKey.cmp a b = 1 := by
  simp [SenderKey.cmp, hACopy, hBNotCopy]

/-- Symmetrically, if b has video copy and a does not, cmp(a,b) = -1. -/
theorem senderKey_copy_preferred_neg
    (a b : SenderKey)
    (hANotCopy : a.videoCodecIsCopy = false)
    (hBCopy : b.videoCodecIsCopy = true) :
    SenderKey.cmp a b = -1 := by
  simp [SenderKey.cmp, hANotCopy, hBCopy]

/-- Audio copy preferred: if audio codecs differ, video matches but copy on a side → result. -/
theorem senderKey_audio_copy_preferred
    (a b : SenderKey)
    (hVCopy : a.videoCodecIsCopy = b.videoCodecIsCopy)
    (hVCodec : a.videoCodec = b.videoCodec)
    (hResEq : a.videoWidth * a.videoHeight = b.videoWidth * b.videoHeight)
    (hACopy : a.audioCodecIsCopy = true)
    (hBACopy : b.audioCodecIsCopy = false) :
    SenderKey.cmp a b = 1 := by
  simp [SenderKey.cmp, SenderKey.compareAudio, hVCopy, hVCodec, hResEq, hACopy, hBACopy]

/-- Antisymmetry for cmp when neither key has copy video codec and video codecs match. -/
theorem senderKey_cmp_antisym_no_copy (a b : SenderKey)
    (hANotCopy : a.videoCodecIsCopy = false)
    (hBNotCopy : b.videoCodecIsCopy = false)
    (hVCodecEq : a.videoCodec = b.videoCodec)
    (hAANotCopy : a.audioCodecIsCopy = false)
    (hBANotCopy : b.audioCodecIsCopy = false)
    (hACodecEq : a.audioCodec = b.audioCodec) :
    SenderKey.cmp a b = -SenderKey.cmp b a := by
  simp [SenderKey.cmp, SenderKey.compareAudio, hANotCopy, hBNotCopy, hVCodecEq,
        hAANotCopy, hBANotCopy, hACodecEq]
  omega

/-! ## 2. Codec Resource Reuse Proofs -/

/-- Width mismatch implies canReuse is false. -/
theorem canReuse_width_mismatch (p : CodecReuseParams)
    (h : p.paramsWidth ≠ p.encoderWidth) :
    canReuse p = false := by
  unfold canReuse
  have : (p.paramsWidth == p.encoderWidth) = false := by
    simp [beq_iff_eq]; exact h
  simp [this]

/-- Height mismatch implies canReuse is false. -/
theorem canReuse_height_mismatch (p : CodecReuseParams)
    (h : p.paramsHeight ≠ p.encoderHeight) :
    canReuse p = false := by
  unfold canReuse
  have : (p.paramsHeight == p.encoderHeight) = false := by
    simp [beq_iff_eq]; exact h
  simp [this]

/-- Pixel format mismatch (non-None) implies canReuse is false. -/
theorem canReuse_pixfmt_mismatch (p : CodecReuseParams)
    (hNotNone : p.paramsPixFmt ≠ 0)
    (hMismatch : p.paramsPixFmt ≠ p.decoderPixFmt) :
    canReuse p = false := by
  unfold canReuse
  have h1 : (p.paramsPixFmt == 0) = false := by simp [beq_iff_eq]; exact hNotNone
  have h2 : (p.paramsPixFmt == p.decoderPixFmt) = false := by simp [beq_iff_eq]; exact hMismatch
  simp [h1, h2]

/-- When all parameters match, canReuse is true. -/
theorem canReuse_all_match (p : CodecReuseParams)
    (hW : p.paramsWidth = p.encoderWidth)
    (hH : p.paramsHeight = p.encoderHeight)
    (hPix : p.paramsPixFmt = 0 ∨ p.paramsPixFmt = p.decoderPixFmt) :
    canReuse p = true := by
  unfold canReuse
  have hw : (p.paramsWidth == p.encoderWidth) = true := by simp [beq_iff_eq]; exact hW
  have hh : (p.paramsHeight == p.encoderHeight) = true := by simp [beq_iff_eq]; exact hH
  have hp : (p.paramsPixFmt == 0 || p.paramsPixFmt == p.decoderPixFmt) = true := by
    simp [beq_iff_eq, Bool.or_eq_true]; exact hPix
  simp [hw, hh, hp]

/-! ## 3. Queue Size Estimation Proofs -/

/-- Zero bitrate means the estimate equals the previous queue size. -/
theorem estimateQueueSize_zero_bitrate (prev timeDelta : Int) :
    estimateQueueSizeSimple prev timeDelta 0 = prev := by
  simp [estimateQueueSizeSimple]

/-- Zero time delta means the estimate equals the previous queue size. -/
theorem estimateQueueSize_zero_timedelta (prev bitRate : Int) :
    estimateQueueSizeSimple prev 0 bitRate = prev := by
  simp [estimateQueueSizeSimple]

/-- With non-negative inputs, the estimate is ≥ previous queue size. -/
theorem estimateQueueSize_ge_prev (prev timeDelta bitRate : Int)
    (hTime : timeDelta ≥ 0) (hRate : bitRate ≥ 0) :
    estimateQueueSizeSimple prev timeDelta bitRate ≥ prev := by
  simp only [estimateQueueSizeSimple]
  have hProd : timeDelta * bitRate ≥ 0 := Int.mul_nonneg hTime hRate
  have hDiv : timeDelta * bitRate / 8 ≥ 0 := Int.ediv_nonneg hProd (by omega)
  omega

/-- The estimate formula is correct by definition. -/
theorem estimateQueueSize_formula (prev timeDelta bitRate : Int) :
    estimateQueueSizeSimple prev timeDelta bitRate = prev + timeDelta * bitRate / 8 := by
  rfl

/-! ## 4. Latency Estimation Proofs -/

/-- Latency is always non-negative (clamped ≥ 0). -/
theorem computeLatency_nonneg (earliest oldest : Int) :
    computeLatency earliest oldest ≥ 0 := by
  simp only [computeLatency]
  split
  · exact Int.le_max_left 0 (earliest - oldest)
  · omega

/-- When oldestDTS ≤ 0, latency is 0. -/
theorem computeLatency_no_queue (earliest oldest : Int) (h : oldest ≤ 0) :
    computeLatency earliest oldest = 0 := by
  simp only [computeLatency]
  omega

/-- When oldestDTS > 0 and earliest ≥ oldest, latency = earliest - oldest. -/
theorem computeLatency_positive (earliest oldest : Int)
    (hOld : oldest > 0) (hGe : earliest ≥ oldest) :
    computeLatency earliest oldest = earliest - oldest := by
  simp only [computeLatency]
  omega

/-- Fallback latency formula is correct by definition. -/
theorem fallbackLatency_formula (prev delta : Int) :
    fallbackLatency prev delta = prev + delta := by
  rfl

/-- Fallback latency ≥ prev when delta ≥ 0. -/
theorem fallbackLatency_ge_prev (prev delta : Int) (hDelta : delta ≥ 0) :
    fallbackLatency prev delta ≥ prev := by
  simp only [fallbackLatency]
  omega

/-! ## 5. AutoBitRateVideoConfig Proofs -/

/-- findConfig on an empty list returns none. -/
theorem findConfig_empty (w h : Int) :
    findConfig [] w h = none := by
  rfl

/-- findConfig returns a config with the requested resolution. -/
theorem findConfig_correct (configs : List ResConfig) (w h : Int) (c : ResConfig)
    (hFind : findConfig configs w h = some c) :
    c.width = w ∧ c.height = h := by
  simp only [findConfig] at hFind
  have hPred := List.find?_some hFind
  simp only [Bool.and_eq_true, beq_iff_eq] at hPred
  exact hPred

/-- bestConfig on empty list is none. -/
theorem bestConfig_empty :
    bestConfig [] = none := by
  rfl

/-- worstConfig on empty list is none. -/
theorem worstConfig_empty :
    worstConfig [] = none := by
  rfl

/-- bestConfig on a singleton is that element. -/
theorem bestConfig_singleton (c : ResConfig) :
    bestConfig [c] = some c := by
  simp [bestConfig, List.foldl]

/-- worstConfig on a singleton is that element. -/
theorem worstConfig_singleton (c : ResConfig) :
    worstConfig [c] = some c := by
  simp [worstConfig, List.foldl]

/-- Helper: foldl for bestConfig preserves `isSome` once the acc is `some`. -/
private theorem bestConfig_foldl_isSome (acc : ResConfig) (rest : List ResConfig) :
    (rest.foldl (fun a c =>
      match a with
      | none => some c
      | some best => if c.pixels > best.pixels then some c else some best
    ) (some acc)).isSome = true := by
  induction rest generalizing acc with
  | nil => rfl
  | cons x xs ih =>
    simp only [List.foldl]
    split
    · exact ih x
    · exact ih _

/-- bestConfig of a non-empty list is some. -/
theorem bestConfig_isSome (c : ResConfig) (rest : List ResConfig) :
    (bestConfig (c :: rest)).isSome = true := by
  simp only [bestConfig, List.foldl]
  exact bestConfig_foldl_isSome c rest

/-- Helper: foldl for worstConfig preserves `isSome` once the acc is `some`. -/
private theorem worstConfig_foldl_isSome (acc : ResConfig) (rest : List ResConfig) :
    (rest.foldl (fun a c =>
      match a with
      | none => some c
      | some worst => if c.pixels < worst.pixels then some c else some worst
    ) (some acc)).isSome = true := by
  induction rest generalizing acc with
  | nil => rfl
  | cons x xs ih =>
    simp only [List.foldl]
    split
    · exact ih x
    · exact ih _

/-- worstConfig of a non-empty list is some. -/
theorem worstConfig_isSome (c : ResConfig) (rest : List ResConfig) :
    (worstConfig (c :: rest)).isSome = true := by
  simp only [worstConfig, List.foldl]
  exact worstConfig_foldl_isSome c rest

/-- Helper: foldl for bestConfig returns a result with pixels ≥ all processed elements. -/
private theorem bestConfig_foldl_ge (acc : ResConfig) (rest : List ResConfig) :
    ∀ r, (rest.foldl (fun a c =>
      match a with
      | none => some c
      | some best => if c.pixels > best.pixels then some c else some best
    ) (some acc)) = some r → r.pixels ≥ acc.pixels ∧ ∀ x ∈ rest, r.pixels ≥ x.pixels := by
  induction rest generalizing acc with
  | nil =>
    intro r hr
    simp [List.foldl] at hr; subst hr
    exact ⟨Int.le_refl _, fun _ h => absurd h (List.not_mem_nil _)⟩
  | cons y ys ih =>
    intro r hr
    simp only [List.foldl] at hr
    by_cases hgt : y.pixels > acc.pixels
    · simp only [hgt, ite_true] at hr
      have ⟨hGeY, hGeYs⟩ := ih y r hr
      exact ⟨by omega, fun x hx => by
        cases List.mem_cons.mp hx with
        | inl heq => subst heq; exact hGeY
        | inr hmem => exact hGeYs x hmem⟩
    · simp only [show ¬(y.pixels > acc.pixels) from hgt, ite_false] at hr
      have ⟨hGeAcc, hGeYs⟩ := ih acc r hr
      exact ⟨hGeAcc, fun x hx => by
        cases List.mem_cons.mp hx with
        | inl heq => subst heq; omega
        | inr hmem => exact hGeYs x hmem⟩

/-- bestConfig returns an element whose pixels ≥ every element in the list. -/
theorem bestConfig_is_max (c : ResConfig) (rest : List ResConfig) (r : ResConfig)
    (hBest : bestConfig (c :: rest) = some r) :
    ∀ x ∈ (c :: rest), r.pixels ≥ x.pixels := by
  simp only [bestConfig, List.foldl] at hBest
  have ⟨hGeC, hGeRest⟩ := bestConfig_foldl_ge c rest r hBest
  intro x hx
  cases List.mem_cons.mp hx with
  | inl heq => subst heq; exact hGeC
  | inr hmem => exact hGeRest x hmem

/-- Helper: foldl for worstConfig returns a result with pixels ≤ all processed elements. -/
private theorem worstConfig_foldl_le (acc : ResConfig) (rest : List ResConfig) :
    ∀ r, (rest.foldl (fun a c =>
      match a with
      | none => some c
      | some worst => if c.pixels < worst.pixels then some c else some worst
    ) (some acc)) = some r → r.pixels ≤ acc.pixels ∧ ∀ x ∈ rest, r.pixels ≤ x.pixels := by
  induction rest generalizing acc with
  | nil =>
    intro r hr
    simp [List.foldl] at hr; subst hr
    exact ⟨Int.le_refl _, fun _ h => absurd h (List.not_mem_nil _)⟩
  | cons y ys ih =>
    intro r hr
    simp only [List.foldl] at hr
    by_cases hlt : y.pixels < acc.pixels
    · simp only [hlt, ite_true] at hr
      have ⟨hLeY, hLeYs⟩ := ih y r hr
      exact ⟨by omega, fun x hx => by
        cases List.mem_cons.mp hx with
        | inl heq => subst heq; exact hLeY
        | inr hmem => exact hLeYs x hmem⟩
    · simp only [show ¬(y.pixels < acc.pixels) from hlt, ite_false] at hr
      have ⟨hLeAcc, hLeYs⟩ := ih acc r hr
      exact ⟨hLeAcc, fun x hx => by
        cases List.mem_cons.mp hx with
        | inl heq => subst heq; omega
        | inr hmem => exact hLeYs x hmem⟩

/-- worstConfig returns an element whose pixels ≤ every element in the list. -/
theorem worstConfig_is_min (c : ResConfig) (rest : List ResConfig) (r : ResConfig)
    (hWorst : worstConfig (c :: rest) = some r) :
    ∀ x ∈ (c :: rest), r.pixels ≤ x.pixels := by
  simp only [worstConfig, List.foldl] at hWorst
  have ⟨hLeC, hLeRest⟩ := worstConfig_foldl_le c rest r hWorst
  intro x hx
  cases List.mem_cons.mp hx with
  | inl heq => subst heq; exact hLeC
  | inr hmem => exact hLeRest x hmem
