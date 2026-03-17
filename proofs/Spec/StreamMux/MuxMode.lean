-- Spec/StreamMux/MuxMode.lean: Formal specification of MuxMode output rules
-- and stream index assignment from preset/streammux/.

/-! ## MuxMode enum

Models the MuxMode iota enum from types/mux_mode.go.
-/
inductive MuxMode where
  | undefined
  | forbid
  | sameOutputSameTracks
  | sameOutputDifferentTracks
  | differentOutputsSameTracks
  | differentOutputsSameTracksSplitAV
  deriving Repr, DecidableEq

/-! ## Media type for SplitAV routing -/
inductive MuxMediaType where
  | video
  | audio
  deriving Repr, DecidableEq

/-! ## Input routing for SplitAV

In SplitAV mode, the output key specifies exactly one of audio or video.
This models the four-way case in getOrCreateOutputLocked lines 600-609.
-/
inductive SplitAVRequest where
  | bothCodecs      -- audioCodec != "" && videoCodec != "" → error
  | audioOnly       -- audioCodec != "" → InputAudioOnly
  | videoOnly       -- videoCodec != "" → InputVideoOnly
  | neitherCodec    -- both empty → error
  deriving Repr, DecidableEq

/-! ## Result of getOrCreateOutputLocked

Models the return type: either success (with the input type used) or an error.
-/
inductive MuxInputKind where
  | all        -- InputAll (all tracks)
  | audioOnly  -- InputAudioOnly
  | videoOnly  -- InputVideoOnly
  deriving Repr, DecidableEq

inductive OutputResult where
  | ok (input : MuxInputKind)
  | existingReturned       -- existing output returned (not new)
  | error (msg : String)
  deriving Repr, DecidableEq

namespace MuxModeSpec

/-! ## Output creation rules

Models the core decision tree in getOrCreateOutputLocked (lines 574-612).
`existingOutputCount` is the number of outputs already in s.Outputs.
-/
def getOrCreateOutput
    (mode : MuxMode)
    (existingOutputCount : Nat)
    (splitReq : SplitAVRequest)
    : OutputResult :=
  match mode with
  | MuxMode.undefined =>
    OutputResult.error "mux mode is not defined"
  | MuxMode.forbid =>
    if existingOutputCount > 0 then
      OutputResult.error "forbid: already have outputs"
    else
      OutputResult.ok MuxInputKind.all
  | MuxMode.sameOutputSameTracks =>
    if existingOutputCount > 1 then
      OutputResult.error "sameOutputSameTracks: too many outputs"
    else if existingOutputCount == 1 then
      OutputResult.existingReturned
    else
      OutputResult.ok MuxInputKind.all
  | MuxMode.sameOutputDifferentTracks =>
    if existingOutputCount > 1 then
      OutputResult.error "sameOutputDifferentTracks: too many outputs"
    else if existingOutputCount == 1 then
      OutputResult.existingReturned
    else
      OutputResult.ok MuxInputKind.all
  | MuxMode.differentOutputsSameTracks =>
    OutputResult.ok MuxInputKind.all
  | MuxMode.differentOutputsSameTracksSplitAV =>
    match splitReq with
    | SplitAVRequest.bothCodecs =>
      OutputResult.error "splitAV: cannot have both audio and video"
    | SplitAVRequest.audioOnly =>
      OutputResult.ok MuxInputKind.audioOnly
    | SplitAVRequest.videoOnly =>
      OutputResult.ok MuxInputKind.videoOnly
    | SplitAVRequest.neitherCodec =>
      OutputResult.error "splitAV: must specify audio or video"

/-! ## Max output count property

Predicate: after a successful creation, does the mode enforce a maximum
number of outputs?
-/

/-- Returns the maximum number of outputs a mode allows, or none for unlimited. -/
def maxOutputs (mode : MuxMode) : Option Nat :=
  match mode with
  | MuxMode.undefined                     => some 0  -- always errors
  | MuxMode.forbid                        => some 1
  | MuxMode.sameOutputSameTracks          => some 1
  | MuxMode.sameOutputDifferentTracks     => some 1
  | MuxMode.differentOutputsSameTracks    => none    -- unlimited
  | MuxMode.differentOutputsSameTracksSplitAV => none -- unlimited (one per AV type)

end MuxModeSpec

/-! ## Stream index assignment

Models StreamIndexAssign from stream_index_assigner.go.

- Most modes: identity mapping (idx → idx)
- SameOutputDifferentTracks with outputID > 0:
    idx → outputID * streamCount + idx
- SameOutputDifferentTracks with outputID = 0: identity
-/
namespace StreamIndexSpec

/-- Stream index assignment function.
    `mode`: the MuxMode
    `outputID`: the output identifier (natural number)
    `streamCount`: number of streams in the format context (positive)
    `inputIdx`: the input stream index
    Returns `none` for error (unknown mode or invalid streamCount).
-/
def assignIndex
    (mode : MuxMode)
    (outputID : Nat)
    (streamCount : Nat)
    (inputIdx : Nat)
    : Option Nat :=
  match mode with
  | MuxMode.undefined => none
  | MuxMode.forbid => some inputIdx
  | MuxMode.sameOutputSameTracks => some inputIdx
  | MuxMode.differentOutputsSameTracks => some inputIdx
  | MuxMode.differentOutputsSameTracksSplitAV => some inputIdx
  | MuxMode.sameOutputDifferentTracks =>
    if outputID == 0 then
      some inputIdx
    else if streamCount == 0 then
      none  -- error: no streams
    else
      some (inputIdx + outputID * streamCount)

/-- Identity modes: all modes except SameOutputDifferentTracks with outputID > 0. -/
def isIdentityMode (mode : MuxMode) : Prop :=
  mode = MuxMode.forbid ∨
  mode = MuxMode.sameOutputSameTracks ∨
  mode = MuxMode.differentOutputsSameTracks ∨
  mode = MuxMode.differentOutputsSameTracksSplitAV

end StreamIndexSpec
