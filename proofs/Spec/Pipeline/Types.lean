-- Spec/Pipeline/Types.lean: Core types for the packet routing model.
-- Models media types, packets, and node identifiers used throughout
-- the pipeline routing proof.

set_option linter.unusedVariables false

/-! ## Media type

Models the media type of a packet, corresponding to Go's
`astiav.MediaType` values used in pipeline routing decisions. -/
inductive PipeMediaType where
  | video
  | audio
  | subtitle
  | data
  deriving DecidableEq, Repr, BEq

/-! ## Packet

Models an AV packet flowing through the pipeline. Corresponds to the
fields of `astiav.Packet` that are relevant to routing decisions. -/
structure PipePacket where
  mediaType   : PipeMediaType
  isKeyFrame  : Bool
  pts         : Int
  dts         : Int
  streamIndex : Nat
  size        : Nat
  deriving DecidableEq, Repr

/-! ## Output identifier

A natural number identifying an output in the streammux. Corresponds
to the map key in `s.Outputs` (Go type `OutputID`). -/
abbrev PipeOutputID := Nat

/-! ## Input kind

Distinguishes the three input fan-in modes used by the streammux.
Corresponds to `InputAll`, `InputAudioOnly`, `InputVideoOnly` in Go. -/
inductive PipeInputKind where
  | all
  | audioOnly
  | videoOnly
  deriving DecidableEq, Repr, BEq

/-! ## Node identifier

Identifies a node in the internal pipeline graph. Each output has two
nodes (an input-side node and a sender-side node), and there are three
global input nodes corresponding to the three `PipeInputKind` values.

Corresponds to the node naming used in `connectOutput` / `rebuildPipeline`. -/
inductive PipeNodeID where
  | inputAll
  | inputAudioOnly
  | inputVideoOnly
  | outputInput  (id : PipeOutputID)
  | outputSender (id : PipeOutputID)
  deriving DecidableEq, Repr, BEq
