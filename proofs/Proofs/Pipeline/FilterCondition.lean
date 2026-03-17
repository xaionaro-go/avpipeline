-- Proofs/Pipeline/FilterCondition.lean: Correctness proofs for filter condition
-- algebra used in packet routing decisions.

import Spec.Pipeline.FilterCondition

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Always / Never basics -/

/-- `always` matches every packet. -/
theorem always_matches (pkt : PipePacket) :
    FilterCondition.always.match pkt = true := by
  rfl

/-- `never` rejects every packet. -/
theorem never_rejects (pkt : PipePacket) :
    FilterCondition.never.match pkt = false := by
  rfl

/-! ## De Morgan laws -/

/-- De Morgan: NOT(AND(a, b)) = OR(NOT a, NOT b). -/
theorem not_and (a b : FilterCondition) (pkt : PipePacket) :
    (FilterCondition.not (FilterCondition.and a b)).match pkt =
    (FilterCondition.or (FilterCondition.not a) (FilterCondition.not b)).match pkt := by
  simp [FilterCondition.match, Bool.not_and]

/-- De Morgan: NOT(OR(a, b)) = AND(NOT a, NOT b). -/
theorem not_or (a b : FilterCondition) (pkt : PipePacket) :
    (FilterCondition.not (FilterCondition.or a b)).match pkt =
    (FilterCondition.and (FilterCondition.not a) (FilterCondition.not b)).match pkt := by
  simp [FilterCondition.match, Bool.not_or]

/-! ## Double negation -/

/-- Double negation elimination: NOT(NOT(c)) = c. -/
theorem not_not (c : FilterCondition) (pkt : PipePacket) :
    (FilterCondition.not (FilterCondition.not c)).match pkt = c.match pkt := by
  simp [FilterCondition.match, Bool.not_not]

/-! ## Media type partition -/

/-- Video packets match `videoCond` and do not match `audioSubtitleDataCond`. -/
theorem partition_video (pkt : PipePacket) (h : pkt.mediaType = .video) :
    videoCond.match pkt = true ∧ audioSubtitleDataCond.match pkt = false := by
  simp only [videoCond, audioSubtitleDataCond, FilterCondition.match, h]
  decide

/-- Audio packets do not match `videoCond` and match `audioSubtitleDataCond`. -/
theorem partition_audio (pkt : PipePacket) (h : pkt.mediaType = .audio) :
    videoCond.match pkt = false ∧ audioSubtitleDataCond.match pkt = true := by
  simp only [videoCond, audioSubtitleDataCond, FilterCondition.match, h]
  decide

/-- Subtitle packets do not match `videoCond` and match `audioSubtitleDataCond`. -/
theorem partition_subtitle (pkt : PipePacket) (h : pkt.mediaType = .subtitle) :
    videoCond.match pkt = false ∧ audioSubtitleDataCond.match pkt = true := by
  simp only [videoCond, audioSubtitleDataCond, FilterCondition.match, h]
  decide

/-- Data packets do not match `videoCond` and match `audioSubtitleDataCond`. -/
theorem partition_data (pkt : PipePacket) (h : pkt.mediaType = .data) :
    videoCond.match pkt = false ∧ audioSubtitleDataCond.match pkt = true := by
  simp only [videoCond, audioSubtitleDataCond, FilterCondition.match, h]
  decide

/-- For every packet, exactly one of `videoCond` and `audioSubtitleDataCond` matches.
    Witnessed by `mediaTypePartition` returning `true`. -/
theorem partition_exhaustive (pkt : PipePacket) :
    mediaTypePartition pkt = true := by
  simp only [mediaTypePartition, videoCond, audioSubtitleDataCond, FilterCondition.match]
  cases pkt.mediaType <;> decide

/-! ## KeepUnless properties -/

private theorem standardKeepUnless_false_eq :
    standardKeepUnless false =
    FilterCondition.and (.mediaType .video) (.or (.isKeyFrame true) .never) := by
  native_decide

/-- Audio packets are rejected by `standardKeepUnless false`. -/
theorem keepUnless_rejects_audio (pkt : PipePacket) (h : pkt.mediaType = .audio) :
    (standardKeepUnless false).match pkt = false := by
  rw [standardKeepUnless_false_eq]
  simp only [FilterCondition.match, h]
  -- Goal: (PipeMediaType.audio == PipeMediaType.video && ...) = false
  -- The LHS of && is false, so the whole && is false.
  simp (config := { decide := true })

/-- Non-keyframe video packets are rejected by `standardKeepUnless false`. -/
theorem keepUnless_rejects_non_keyframe_video (pkt : PipePacket)
    (hmt : pkt.mediaType = .video) (hkf : pkt.isKeyFrame = false) :
    (standardKeepUnless false).match pkt = false := by
  rw [standardKeepUnless_false_eq]
  simp only [FilterCondition.match, hmt, hkf]
  decide

/-- Keyframe video packets are accepted by `standardKeepUnless false`. -/
theorem keepUnless_accepts_keyframe_video (pkt : PipePacket)
    (hmt : pkt.mediaType = .video) (hkf : pkt.isKeyFrame = true) :
    (standardKeepUnless false).match pkt = true := by
  rw [standardKeepUnless_false_eq]
  simp only [FilterCondition.match, hmt, hkf]
  decide

end Pipeline
