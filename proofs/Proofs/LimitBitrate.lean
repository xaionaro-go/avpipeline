-- Proofs/LimitBitrate.lean: Correctness proofs for the LimitVideoBitrate filter

import Spec.LimitBitrate

open LimitBitrateState InputKind LimitBitrateConfig

/-! ## Zero bitrate (disabled) always passes -/

theorem disabled_filter_passes (s : LimitBitrateState) (input : InputKind) (allowed : Nat) :
    (s.step ⟨0, 0⟩ input allowed).1 = true := by
  simp [step]

theorem disabled_filter_preserves_state (s : LimitBitrateState) (input : InputKind)
    (allowed : Nat) :
    (s.step ⟨0, 0⟩ input allowed).2 = s := by
  simp [step]

/-! ## Non-video input always passes -/

theorem non_video_passes (s : LimitBitrateState) (cfg : LimitBitrateConfig) (allowed : Nat) :
    (s.step cfg InputKind.nonVideo allowed).1 = true := by
  unfold step; split <;> rfl

theorem non_video_preserves_state (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (allowed : Nat) :
    (s.step cfg InputKind.nonVideo allowed).2 = s := by
  unfold step; split <;> rfl

/-! ## Video frame (non-packet) always passes -/

theorem video_frame_passes (s : LimitBitrateState) (cfg : LimitBitrateConfig) (allowed : Nat) :
    (s.step cfg InputKind.videoFrame allowed).1 = true := by
  unfold step; split <;> rfl

theorem video_frame_preserves_state (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (allowed : Nat) :
    (s.step cfg InputKind.videoFrame allowed).2 = s := by
  unfold step; split <;> rfl

/-! ## Drain properties -/

theorem drain_clamps_to_zero (s : LimitBitrateState) (n : Nat) :
    (s.drain n).consumed ≤ s.consumed := by
  simp [drain]

theorem drain_zero_is_identity (s : LimitBitrateState) :
    s.drain 0 = s := by
  simp [drain]

theorem drain_large_zeros_consumed (s : LimitBitrateState) (n : Nat) (h : n ≥ s.consumed) :
    (s.drain n).consumed = 0 := by
  simp [drain]; omega

theorem drain_preserves_skippedFrame (s : LimitBitrateState) (n : Nat) :
    (s.drain n).skippedFrame = s.skippedFrame := by
  simp [drain]

theorem drain_monotone (s : LimitBitrateState) (a b : Nat) (h : a ≤ b) :
    (s.drain b).consumed ≤ (s.drain a).consumed := by
  simp [drain]; omega

/-! ## Empty bucket + keyframe always accepted (keyframe exception) -/

theorem empty_bucket_keyframe_accepted (cfg : LimitBitrateConfig) (sizeBits : Nat) :
    let s : LimitBitrateState := ⟨0, false⟩
    (s.processVideoPacket cfg sizeBits true).1 = true := by
  simp [processVideoPacket]

theorem empty_bucket_keyframe_consumed (cfg : LimitBitrateConfig) (sizeBits : Nat) :
    let s : LimitBitrateState := ⟨0, false⟩
    (s.processVideoPacket cfg sizeBits true).2.consumed = sizeBits := by
  simp [processVideoPacket]

/-! ## Packet within budget is accepted (no skip streak) -/

theorem within_budget_accepted (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hNoSkip : s.skippedFrame = false)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits isKey).1 = true := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this, hNoSkip]

theorem accepted_updates_consumed (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hNoSkip : s.skippedFrame = false)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits isKey).2.consumed = s.consumed + sizeBits := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this, hNoSkip]

theorem accepted_clears_skip (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hNoSkip : s.skippedFrame = false)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits isKey).2.skippedFrame = false := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this, hNoSkip]

/-! ## Overflow with non-keyframe rejects -/

theorem overflow_non_keyframe_rejected (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hOver : s.consumed + sizeBits > cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits false).1 = false := by
  simp only [processVideoPacket]
  simp [decide_eq_true hOver]

theorem overflow_non_keyframe_sets_skip (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hOver : s.consumed + sizeBits > cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits false).2.skippedFrame = true := by
  simp only [processVideoPacket]
  simp [decide_eq_true hOver]

/-! ## Overflow with keyframe and non-empty bucket rejects -/

theorem overflow_keyframe_nonempty_rejected (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hOver : s.consumed + sizeBits > cfg.averagingBufferBits)
    (hNonEmpty : s.consumed ≠ 0) :
    (s.processVideoPacket cfg sizeBits true).1 = false := by
  simp only [processVideoPacket]
  have hne : (s.consumed != 0) = true := by simp [bne_iff_ne, hNonEmpty]
  simp [decide_eq_true hOver, hne]

/-! ## Skip streak: non-keyframe rejected even if within budget -/

theorem skip_streak_non_keyframe_rejected (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hSkip : s.skippedFrame = true)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits false).1 = false := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this, hSkip]

/-! ## Skip streak: keyframe within budget breaks the streak -/

theorem skip_streak_keyframe_accepted (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits true).1 = true := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this]

theorem skip_streak_keyframe_clears_skip (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hFits : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits true).2.skippedFrame = false := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this]

/-! ## Sufficient drain guarantees acceptance -/

theorem full_drain_accepts (cfg : LimitBitrateConfig)
    (hRate : cfg.averageBitRate > 0)
    (sizeBits : Nat) (isKey : Bool) (allowed : Nat)
    (hFits : sizeBits ≤ cfg.averagingBufferBits) :
    let s : LimitBitrateState := ⟨0, false⟩
    (s.step cfg (InputKind.videoPacket sizeBits isKey) allowed).1 = true := by
  simp only [step]
  have hNz : ¬ cfg.averageBitRate = 0 := by omega
  simp only [beq_iff_eq, hNz, ↓reduceIte, drain, processVideoPacket]
  simp only [Nat.zero_sub]
  have := decide_eq_false (show ¬ sizeBits > cfg.averagingBufferBits by omega)
  simp [this]

/-! ## Initial state accepts any packet that fits in the buffer -/

theorem init_accepts_fitting_packet (cfg : LimitBitrateConfig)
    (hRate : cfg.averageBitRate > 0)
    (sizeBits : Nat) (isKey : Bool) (allowed : Nat)
    (hFits : sizeBits ≤ cfg.averagingBufferBits) :
    (LimitBitrateState.init.step cfg (InputKind.videoPacket sizeBits isKey) allowed).1
      = true := by
  simp only [step, init]
  have hNz : ¬ cfg.averageBitRate = 0 := by omega
  simp only [beq_iff_eq, hNz, ↓reduceIte, drain, processVideoPacket]
  simp only [Nat.zero_sub]
  have := decide_eq_false (show ¬ sizeBits > cfg.averagingBufferBits by omega)
  simp [this]

/-! ## Reject implies overflow or skip streak -/

theorem reject_implies_overflow_or_skip (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hRej : (s.processVideoPacket cfg sizeBits isKey).1 = false) :
    (s.consumed + sizeBits > cfg.averagingBufferBits ∧ (!isKey || (s.consumed != 0)) = true)
    ∨ (s.skippedFrame = true ∧ isKey = false) := by
  simp only [processVideoPacket] at hRej
  by_cases hOv : s.consumed + sizeBits > cfg.averagingBufferBits
  · have hd := decide_eq_true hOv
    by_cases hGuard : (!isKey || (s.consumed != 0)) = true
    · left; exact ⟨hOv, hGuard⟩
    · simp [Bool.or_eq_true, bne_iff_ne] at hGuard
      obtain ⟨hKey, hZero⟩ := hGuard
      simp [hd, hKey, hZero] at hRej
  · have hd := decide_eq_false hOv
    simp [hd] at hRej
    right
    split at hRej
    · rename_i h; exact h
    · simp at hRej

/-! ## The full step decomposes into drain then process -/

theorem step_is_drain_then_process (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (hRate : cfg.averageBitRate > 0) (sizeBits : Nat) (isKey : Bool) (allowed : Nat) :
    s.step cfg (InputKind.videoPacket sizeBits isKey) allowed =
      (s.drain allowed).processVideoPacket cfg sizeBits isKey := by
  simp only [step]
  have hNz : ¬ cfg.averageBitRate = 0 := by omega
  simp [beq_iff_eq, hNz]

/-! ## Keyframe exception: empty bucket keyframe always accepted, even oversized -/

theorem keyframe_exception (cfg : LimitBitrateConfig) (sizeBits : Nat)
    (_hOver : sizeBits > cfg.averagingBufferBits) :
    let s : LimitBitrateState := ⟨0, false⟩
    (s.processVideoPacket cfg sizeBits true).1 = true ∧
    (s.processVideoPacket cfg sizeBits true).2.consumed = sizeBits := by
  simp [processVideoPacket]

/-! ## Acceptance always clears skip streak -/

theorem accept_clears_skip_general (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hAcc : (s.processVideoPacket cfg sizeBits isKey).1 = true) :
    (s.processVideoPacket cfg sizeBits isKey).2.skippedFrame = false := by
  simp only [processVideoPacket] at hAcc ⊢
  split
  · rename_i h; simp [h] at hAcc
  · split
    · rename_i h1 h2; simp [h1, h2] at hAcc
    · rfl

/-! ## Overflow rejection preserves consumed -/

theorem overflow_reject_preserves_consumed (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKey : Bool)
    (hOver : s.consumed + sizeBits > cfg.averagingBufferBits)
    (hGuard : (!isKey || (s.consumed != 0)) = true) :
    (s.processVideoPacket cfg sizeBits isKey).2.consumed = s.consumed := by
  simp only [processVideoPacket]
  simp [decide_eq_true hOver, hGuard]

/-! ## Skip-streak rejection preserves full state -/

theorem skip_streak_reject_preserves_state (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat)
    (hSkip : s.skippedFrame = true)
    (hNoOverflow : s.consumed + sizeBits ≤ cfg.averagingBufferBits) :
    (s.processVideoPacket cfg sizeBits false).2 = s := by
  simp only [processVideoPacket]
  have := decide_eq_false (show ¬ s.consumed + sizeBits > cfg.averagingBufferBits by omega)
  simp [this, hSkip]
