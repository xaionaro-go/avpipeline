-- Spec/Pipeline/Wiring.lean: Per-MuxMode graph construction and operational delivery.
-- Models how the streammux wires input nodes to output nodes depending on
-- the MuxMode, and traces packet delivery through the resulting graph.

import Spec.Pipeline.Types
import Spec.Pipeline.FilterCondition
import Spec.Pipeline.Barrier
import Spec.Pipeline.OutputChain
import Spec.StreamMux.MuxMode

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Routing edge

A directed edge in the pipeline graph, connecting a source node to a
destination node with a filter condition that gates packet flow.
Corresponds to one `AddPushTo` call in Go. -/
structure RoutingEdge where
  src       : PipeNodeID
  dst       : PipeNodeID
  condition : FilterCondition
  deriving Repr

/-! ## Wired pipeline

The fully wired pipeline graph for a given MuxMode configuration.
Captures the edges between nodes, the output/switch state, and
the per-output processing chains. -/
structure WiredPipeline where
  mode              : MuxMode
  outputs           : List PipeOutputID
  audioOutputs      : List PipeOutputID
  videoOutputs      : List PipeOutputID
  switchState       : SwitchState    -- switch for InputAll (non-SplitAV modes)
  audioSwitchState  : SwitchState    -- switch for audio input (SplitAV)
  videoSwitchState  : SwitchState    -- switch for video input (SplitAV)
  syncerState       : SwitchState
  edges             : List RoutingEdge
  outputChains      : PipeOutputID → OutputChain

/-- Select the appropriate switch state based on the parent (source) node.
    In SplitAV mode, audio and video inputs have independent switches.
    All other nodes use the default `switchState`. -/
def WiredPipeline.switchFor (wp : WiredPipeline) (parentNode : PipeNodeID) : SwitchState :=
  match parentNode with
  | .inputAudioOnly => wp.audioSwitchState
  | .inputVideoOnly => wp.videoSwitchState
  | _               => wp.switchState

/-! ## Wire constructors

Factory functions that build a `WiredPipeline` for each MuxMode
topology. -/

/-- Wire a single-output pipeline (Forbid, SameOutputSameTracks,
    SameOutputDifferentTracks). One edge from `inputAll` to
    `outputInput outID` with an `always` condition. Switch stable
    at `outID`. -/
def wireSingleOutput (mode : MuxMode) (outID : PipeOutputID)
    (chain : OutputChain) : WiredPipeline :=
  let sw : SwitchState := ⟨outID, none, outID, .always⟩
  { mode              := mode
    outputs           := [outID]
    audioOutputs      := []
    videoOutputs      := []
    switchState       := sw
    audioSwitchState  := sw
    videoSwitchState  := sw
    syncerState       := sw
    edges             := [{ src := .inputAll
                            dst := .outputInput outID
                            condition := .always }]
    outputChains      := fun _ => chain }

/-- Wire a multi-output pipeline (DifferentOutputsSameTracks).
    One edge per output from `inputAll` to `outputInput id` with
    `always` condition. Switch at `activeID` with `standardKeepUnless`. -/
def wireMultiOutput (outIDs : List PipeOutputID)
    (activeID : PipeOutputID)
    (chain : PipeOutputID → OutputChain) : WiredPipeline :=
  let sw : SwitchState := ⟨activeID, none, activeID, standardKeepUnless false⟩
  { mode              := .differentOutputsSameTracks
    outputs           := outIDs
    audioOutputs      := []
    videoOutputs      := []
    switchState       := sw
    audioSwitchState  := sw
    videoSwitchState  := sw
    syncerState       := sw
    edges             := outIDs.map fun id =>
                           { src := .inputAll
                             dst := .outputInput id
                             condition := .always }
    outputChains      := chain }

/-- Wire a split-AV pipeline (DifferentOutputsSameTracksSplitAV).
    Edges: inputAll→inputAudioOnly (audioSubtitleDataCond),
    inputAll→inputVideoOnly (videoCond), then inputAudioOnly→each
    audio output, inputVideoOnly→each video output. -/
def wireSplitAV (audioOuts videoOuts : List PipeOutputID)
    (activeAudio activeVideo : PipeOutputID)
    (chain : PipeOutputID → OutputChain) : WiredPipeline :=
  let splitEdges : List RoutingEdge :=
    [ { src := .inputAll,       dst := .inputAudioOnly, condition := audioSubtitleDataCond }
    , { src := .inputAll,       dst := .inputVideoOnly, condition := videoCond } ]
  let audioEdges : List RoutingEdge :=
    audioOuts.map fun id =>
      { src := .inputAudioOnly, dst := .outputInput id,  condition := .always }
  let videoEdges : List RoutingEdge :=
    videoOuts.map fun id =>
      { src := .inputVideoOnly, dst := .outputInput id,  condition := .always }
  let audioSw : SwitchState := ⟨activeAudio, none, activeAudio, standardKeepUnless false⟩
  let videoSw : SwitchState := ⟨activeVideo, none, activeVideo, standardKeepUnless false⟩
  { mode              := .differentOutputsSameTracksSplitAV
    outputs           := audioOuts ++ videoOuts
    audioOutputs      := audioOuts
    videoOutputs      := videoOuts
    switchState       := audioSw
    audioSwitchState  := audioSw
    videoSwitchState  := videoSw
    syncerState       := videoSw
    edges             := splitEdges ++ audioEdges ++ videoEdges
    outputChains      := chain }

/-! ## Edge query -/

/-- Return all edges whose source matches `src`. -/
def WiredPipeline.edgesFrom (wp : WiredPipeline) (src : PipeNodeID)
    : List RoutingEdge :=
  wp.edges.filter fun e => e.src == src

/-! ## Delivery trace

Recursively traces which outputs a packet reaches when injected at a
given source node, following matching edges through the graph. -/

/-- Trace delivery of `pkt` from `src` through the wired pipeline.
    Returns the list of output IDs that the packet reaches.
    `parentNode` tracks which node we traversed from, so that in SplitAV
    mode we select the correct per-input switch (audio vs video).
    `fuel` bounds recursion depth to guarantee termination. -/
def traceDelivery (wp : WiredPipeline) (src : PipeNodeID)
    (pkt : PipePacket) (parentNode : PipeNodeID := .inputAll)
    (fuel : Nat := 3) : List PipeOutputID :=
  match fuel with
  | 0 => []
  | fuel' + 1 =>
    let matchingEdges := wp.edgesFrom src
      |>.filter fun e => e.condition.match pkt
    matchingEdges.flatMap fun e =>
      match e.dst with
      | .outputInput id =>
        let sw := wp.switchFor src
        let (_, decision) := outputSwitchGetState sw id pkt
        match decision with
        | .pass => [id]
        | _     => []
      | other => traceDelivery wp other pkt src fuel'

/-! ## Operational delivery

The top-level delivery function: inject a packet at `inputAll` and
collect all outputs it reaches. -/
def operationalDelivery (wp : WiredPipeline) (pkt : PipePacket)
    : List PipeOutputID :=
  traceDelivery wp .inputAll pkt

end Pipeline
