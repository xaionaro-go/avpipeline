-- Spec/Pipeline/FilterCondition.lean: Filter condition algebra for packet routing.
-- Models the Boolean predicates used by the streammux to decide which
-- packets flow to which outputs, corresponding to Go's filter functions.

import Spec.Pipeline.Types

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Filter condition

An inductive type representing composable Boolean predicates over packets.
Corresponds to the `filterCondition` closures passed to `AddPushTo` in Go. -/
inductive FilterCondition where
  | always
  | never
  | mediaType (mt : PipeMediaType)
  | isKeyFrame (kf : Bool)
  | and (a b : FilterCondition)
  | or (a b : FilterCondition)
  | not (c : FilterCondition)
  deriving DecidableEq, Repr

/-! ## Evaluation

Recursively evaluate a `FilterCondition` against a concrete packet. -/
def FilterCondition.match (cond : FilterCondition) (pkt : PipePacket) : Bool :=
  match cond with
  | .always        => true
  | .never         => false
  | .mediaType mt  => pkt.mediaType == mt
  | .isKeyFrame k  => pkt.isKeyFrame == k
  | .and a b       => a.match pkt && b.match pkt
  | .or a b        => a.match pkt || b.match pkt
  | .not c         => !c.match pkt

/-! ## Standard conditions

Named conditions that mirror the routing logic in Go's `connectOutput`. -/

/-- Matches audio, subtitle, or data packets. Corresponds to the
    `audioSubtitleDataCond` filter in Go's streammux. -/
def audioSubtitleDataCond : FilterCondition :=
  .or (.mediaType .audio) (.or (.mediaType .subtitle) (.mediaType .data))

/-- Matches video packets. -/
def videoCond : FilterCondition :=
  .mediaType .video

/-- The "keep unless corrupt" filter: video packets that are either
    keyframes or (if `allowCorrupt`) any frame at all. -/
def standardKeepUnless (allowCorrupt : Bool) : FilterCondition :=
  .and (.mediaType .video) (.or (.isKeyFrame true) (if allowCorrupt then .always else .never))

/-! ## Partition property

Every packet is routed to exactly one of the two branches (audio/subtitle/data
vs. video). This function witnesses the partition: for any packet, one
condition matches and the other does not. -/
def mediaTypePartition (pkt : PipePacket) : Bool :=
  audioSubtitleDataCond.match pkt != videoCond.match pkt

end Pipeline
