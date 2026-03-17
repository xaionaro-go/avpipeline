-- Proofs/StreamMux/MuxMode.lean: Correctness proofs for MuxMode output rules
-- and stream index assignment.

import Spec.StreamMux.MuxMode

open MuxModeSpec StreamIndexSpec OutputResult MuxInputKind MuxMode SplitAVRequest

/-! ## MuxMode Output Rules -/

/-! ### UndefinedMuxMode always errors -/

theorem undefined_always_error (n : Nat) (req : SplitAVRequest) :
    ∃ msg, getOrCreateOutput undefined n req = error msg := by
  exact ⟨_, rfl⟩

/-! ### MuxModeForbid: max 1 output -/

/-- Forbid errors when outputs already exist. -/
theorem forbid_rejects_when_outputs_exist (n : Nat) (req : SplitAVRequest)
    (hn : n > 0) :
    ∃ msg, getOrCreateOutput forbid n req = error msg := by
  simp only [getOrCreateOutput, hn, ↓reduceIte]
  exact ⟨_, rfl⟩

/-- Forbid succeeds with zero existing outputs, using InputAll. -/
theorem forbid_succeeds_with_zero (req : SplitAVRequest) :
    getOrCreateOutput forbid 0 req = ok all := by
  rfl

/-- Forbid allows at most 1 output: after success, existingOutputCount was 0. -/
theorem forbid_max_one_output (n : Nat) (req : SplitAVRequest)
    (h : ∃ ik, getOrCreateOutput forbid n req = ok ik) :
    n = 0 := by
  simp only [getOrCreateOutput] at h
  split at h
  · obtain ⟨_, h⟩ := h; simp at h
  · rename_i hlt; omega

/-! ### SameOutputSameTracks: max 1 output -/

/-- SameOutputSameTracks errors when more than 1 output exists. -/
theorem sameOutputSameTracks_errors_over_one (n : Nat) (req : SplitAVRequest)
    (hn : n > 1) :
    ∃ msg, getOrCreateOutput sameOutputSameTracks n req = error msg := by
  simp only [getOrCreateOutput, hn, ↓reduceIte]
  exact ⟨_, rfl⟩

/-- SameOutputSameTracks returns existing when exactly 1 output. -/
theorem sameOutputSameTracks_returns_existing (req : SplitAVRequest) :
    getOrCreateOutput sameOutputSameTracks 1 req = existingReturned := by
  cases req <;> decide

/-- SameOutputSameTracks succeeds with zero outputs, using InputAll. -/
theorem sameOutputSameTracks_succeeds_zero (req : SplitAVRequest) :
    getOrCreateOutput sameOutputSameTracks 0 req = ok all := by
  cases req <;> decide

/-- SameOutputSameTracks: successful creation only when n = 0. -/
theorem sameOutputSameTracks_max_one (n : Nat) (req : SplitAVRequest)
    (h : ∃ ik, getOrCreateOutput sameOutputSameTracks n req = ok ik) :
    n = 0 := by
  simp only [getOrCreateOutput] at h
  split at h
  · obtain ⟨_, h⟩ := h; simp at h
  · rename_i hgt
    split at h
    · obtain ⟨_, h⟩ := h; simp at h
    · rename_i hbeq
      simp [Nat.beq_eq] at hbeq
      omega

/-! ### SameOutputDifferentTracks: max 1 output -/

/-- SameOutputDifferentTracks errors when more than 1 output exists. -/
theorem sameOutputDifferentTracks_errors_over_one (n : Nat) (req : SplitAVRequest)
    (hn : n > 1) :
    ∃ msg, getOrCreateOutput sameOutputDifferentTracks n req = error msg := by
  simp only [getOrCreateOutput, hn, ↓reduceIte]
  exact ⟨_, rfl⟩

/-- SameOutputDifferentTracks returns existing when exactly 1 output. -/
theorem sameOutputDifferentTracks_returns_existing (req : SplitAVRequest) :
    getOrCreateOutput sameOutputDifferentTracks 1 req = existingReturned := by
  cases req <;> decide

/-- SameOutputDifferentTracks: successful creation only when n = 0. -/
theorem sameOutputDifferentTracks_max_one (n : Nat) (req : SplitAVRequest)
    (h : ∃ ik, getOrCreateOutput sameOutputDifferentTracks n req = ok ik) :
    n = 0 := by
  simp only [getOrCreateOutput] at h
  split at h
  · obtain ⟨_, h⟩ := h; simp at h
  · rename_i hgt
    split at h
    · obtain ⟨_, h⟩ := h; simp at h
    · rename_i hbeq
      simp [Nat.beq_eq] at hbeq
      omega

/-! ### DifferentOutputsSameTracks: unlimited outputs -/

/-- DifferentOutputsSameTracks always succeeds regardless of existing count. -/
theorem differentOutputsSameTracks_always_succeeds (n : Nat) (req : SplitAVRequest) :
    getOrCreateOutput differentOutputsSameTracks n req = ok all := by
  rfl

/-! ### SplitAV: video and audio routed to separate inputs -/

/-- SplitAV with audioOnly request routes to audioOnly input. -/
theorem splitAV_audio_routes_audio (n : Nat) :
    getOrCreateOutput differentOutputsSameTracksSplitAV n audioOnly
      = ok MuxInputKind.audioOnly := by
  rfl

/-- SplitAV with videoOnly request routes to videoOnly input. -/
theorem splitAV_video_routes_video (n : Nat) :
    getOrCreateOutput differentOutputsSameTracksSplitAV n videoOnly
      = ok MuxInputKind.videoOnly := by
  rfl

/-- SplitAV rejects requests with both codecs. -/
theorem splitAV_rejects_both (n : Nat) :
    ∃ msg, getOrCreateOutput differentOutputsSameTracksSplitAV n bothCodecs
      = error msg := by
  exact ⟨_, rfl⟩

/-- SplitAV rejects requests with neither codec. -/
theorem splitAV_rejects_neither (n : Nat) :
    ∃ msg, getOrCreateOutput differentOutputsSameTracksSplitAV n neitherCodec
      = error msg := by
  exact ⟨_, rfl⟩

/-- SplitAV: audio and video are routed to different input kinds. -/
theorem splitAV_separates_av (n : Nat) :
    getOrCreateOutput differentOutputsSameTracksSplitAV n audioOnly ≠
    getOrCreateOutput differentOutputsSameTracksSplitAV n videoOnly := by
  simp [getOrCreateOutput]

/-! ## maxOutputs consistency -/

/-- Undefined mode has maxOutputs = 0, consistent with always-error. -/
theorem undefined_maxOutputs :
    maxOutputs undefined = some 0 := by
  rfl

/-- Forbid, SameOutputSameTracks, SameOutputDifferentTracks have max 1. -/
theorem single_output_modes :
    maxOutputs forbid = some 1 ∧
    maxOutputs sameOutputSameTracks = some 1 ∧
    maxOutputs sameOutputDifferentTracks = some 1 := by
  exact ⟨rfl, rfl, rfl⟩

/-- DifferentOutputsSameTracks and SplitAV are unlimited. -/
theorem unlimited_modes :
    maxOutputs differentOutputsSameTracks = none ∧
    maxOutputs differentOutputsSameTracksSplitAV = none := by
  exact ⟨rfl, rfl⟩

/-! ## Stream Index Assignment -/

/-! ### Identity modes: index passes through unchanged -/

/-- For identity modes, assignIndex returns the input index unchanged. -/
theorem identity_mode_passthrough (mode : MuxMode) (outputID streamCount inputIdx : Nat)
    (hMode : isIdentityMode mode) :
    assignIndex mode outputID streamCount inputIdx = some inputIdx := by
  rcases hMode with rfl | rfl | rfl | rfl <;> simp [assignIndex]

/-- Forbid is identity. -/
theorem forbid_identity (outputID sc idx : Nat) :
    assignIndex forbid outputID sc idx = some idx := by
  simp [assignIndex]

/-- SameOutputSameTracks is identity. -/
theorem sameOutputSameTracks_identity (outputID sc idx : Nat) :
    assignIndex sameOutputSameTracks outputID sc idx = some idx := by
  simp [assignIndex]

/-- DifferentOutputsSameTracks is identity. -/
theorem differentOutputsSameTracks_identity (outputID sc idx : Nat) :
    assignIndex differentOutputsSameTracks outputID sc idx = some idx := by
  simp [assignIndex]

/-- SplitAV is identity. -/
theorem splitAV_identity (outputID sc idx : Nat) :
    assignIndex differentOutputsSameTracksSplitAV outputID sc idx = some idx := by
  simp [assignIndex]

/-! ### SameOutputDifferentTracks: outputID=0 is identity -/

theorem sameOutputDifferentTracks_zero_identity (sc idx : Nat) :
    assignIndex sameOutputDifferentTracks 0 sc idx = some idx := by
  simp [assignIndex]

/-! ### SameOutputDifferentTracks: outputID>0 computes offset -/

theorem sameOutputDifferentTracks_offset (outputID sc idx : Nat)
    (hOut : outputID > 0) (hSc : sc > 0) :
    assignIndex sameOutputDifferentTracks outputID sc idx
      = some (idx + outputID * sc) := by
  simp only [assignIndex]
  have h1 : ¬ (outputID == 0) = true := by simp [Nat.beq_eq]; omega
  have h2 : ¬ (sc == 0) = true := by simp [Nat.beq_eq]; omega
  simp [h1, h2]

/-! ### SameOutputDifferentTracks: error on zero streamCount with outputID>0 -/

theorem sameOutputDifferentTracks_error_zero_streams (outputID : Nat)
    (hOut : outputID > 0) (idx : Nat) :
    assignIndex sameOutputDifferentTracks outputID 0 idx = none := by
  simp only [assignIndex]
  have h1 : ¬ (outputID == 0) = true := by simp [Nat.beq_eq]; omega
  simp [h1]

/-! ### Undefined mode always errors -/

theorem undefined_assignIndex_error (outputID sc idx : Nat) :
    assignIndex undefined outputID sc idx = none := by
  rfl

/-! ### Mapped indices are non-negative (always true for Nat, but we prove
    the result is `some v` for valid configurations) -/

/-- For any valid (non-undefined) mode with positive streamCount,
    assignIndex produces a result. -/
theorem valid_mode_produces_result (mode : MuxMode) (outputID sc idx : Nat)
    (hMode : mode ≠ undefined) (hSc : sc > 0) :
    ∃ v, assignIndex mode outputID sc idx = some v := by
  cases mode with
  | undefined => contradiction
  | forbid => exact ⟨idx, rfl⟩
  | sameOutputSameTracks => exact ⟨idx, rfl⟩
  | differentOutputsSameTracks => exact ⟨idx, rfl⟩
  | differentOutputsSameTracksSplitAV => exact ⟨idx, rfl⟩
  | sameOutputDifferentTracks =>
    simp only [assignIndex]
    by_cases h0 : (outputID == 0) = true
    · simp [h0]
    · have h2 : ¬ (sc == 0) = true := by simp [Nat.beq_eq]; omega
      simp [h0, h2]

/-! ### Uniqueness of mapped indices per output -/

/-- For identity modes, distinct input indices map to distinct output indices. -/
theorem identity_unique (mode : MuxMode) (outputID sc : Nat)
    (idx1 idx2 : Nat) (hMode : isIdentityMode mode)
    (hNeq : idx1 ≠ idx2) :
    assignIndex mode outputID sc idx1 ≠ assignIndex mode outputID sc idx2 := by
  rcases hMode with rfl | rfl | rfl | rfl <;> simp [assignIndex, hNeq]

/-- Rewrite assignIndex for sameOutputDifferentTracks into a simple if-then-else
    when streamCount > 0. -/
private theorem assign_diff_tracks (outputID sc idx : Nat)
    (hSc : sc > 0) :
    assignIndex sameOutputDifferentTracks outputID sc idx =
      if outputID = 0 then some idx else some (idx + outputID * sc) := by
  simp only [assignIndex]
  by_cases h0 : outputID = 0
  · subst h0; simp
  · have h1 : ¬ (outputID == 0) = true := by simp [Nat.beq_eq]; exact h0
    have h2 : ¬ (sc == 0) = true := by simp [Nat.beq_eq]; omega
    simp [h1, h2, h0]

/-- For SameOutputDifferentTracks, distinct (outputID, idx) pairs with
    idx < streamCount produce distinct mapped indices. This models the
    formula idx + outputID * streamCount partitioning the index space. -/
theorem different_tracks_unique_across_outputs
    (out1 out2 : Nat) (sc idx1 idx2 : Nat)
    (hSc : sc > 0)
    (hIdx1 : idx1 < sc) (hIdx2 : idx2 < sc)
    (hPair : out1 ≠ out2 ∨ idx1 ≠ idx2) :
    assignIndex sameOutputDifferentTracks out1 sc idx1 ≠
    assignIndex sameOutputDifferentTracks out2 sc idx2 := by
  rw [assign_diff_tracks out1 sc idx1 hSc, assign_diff_tracks out2 sc idx2 hSc]
  by_cases h1 : out1 = 0 <;> by_cases h2 : out2 = 0 <;> simp [h1, h2]
  · -- out1 = 0, out2 = 0: identity for both, indices must differ
    subst h1; subst h2; simp at hPair; exact hPair
  · -- out1 = 0, out2 ≠ 0: idx1 vs idx2 + out2 * sc
    intro heq
    have hge : out2 * sc ≥ 1 * sc := Nat.mul_le_mul_right sc (by omega)
    simp at hge; omega
  · -- out1 ≠ 0, out2 = 0: idx1 + out1 * sc vs idx2
    intro heq
    have hge : out1 * sc ≥ 1 * sc := Nat.mul_le_mul_right sc (by omega)
    simp at hge; omega
  · -- out1 ≠ 0, out2 ≠ 0: use modular arithmetic to show contradiction
    intro heq
    have hmod1 : (idx1 + out1 * sc) % sc = idx1 := by
      rw [Nat.add_mul_mod_self_right]; exact Nat.mod_eq_of_lt hIdx1
    have hmod2 : (idx2 + out2 * sc) % sc = idx2 := by
      rw [Nat.add_mul_mod_self_right]; exact Nat.mod_eq_of_lt hIdx2
    rw [heq] at hmod1; rw [hmod1] at hmod2; subst hmod2
    have hmul : out1 * sc = out2 * sc := by omega
    have hout : out1 = out2 := Nat.mul_right_cancel hSc hmul
    rcases hPair with h | h
    · exact h hout
    · exact h rfl
