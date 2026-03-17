-- Spec/Pipeline/Routing.lean: Declarative routing specification and
-- well-formedness for the streammux packet routing model.
-- Defines what the routing *should* do for each MuxMode, and the
-- well-formedness conditions required for correctness.
-- Theorem statements and proofs live in Proofs/Pipeline/Routing.lean.

import Spec.Pipeline.Types
import Spec.Pipeline.FilterCondition
import Spec.Pipeline.Barrier
import Spec.Pipeline.OutputChain
import Spec.Pipeline.Wiring
import Spec.StreamMux.MuxMode

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Declarative routing specification

`routingSpec` defines the *intended* set of output IDs that a packet should
reach, purely as a function of the MuxMode, the pipeline's output list, and
the switch state. This is the "what should happen" against which we verify
the operational model (`operationalDelivery`). -/

/-- Declarative specification: which outputs should receive `pkt` given the
    wired pipeline configuration `wp`. -/
def routingSpec (wp : WiredPipeline) (pkt : PipePacket) : List PipeOutputID :=
  match wp.mode with
  | .undefined                        => []
  | .forbid                           => []
  | .sameOutputSameTracks             => wp.outputs
  | .sameOutputDifferentTracks        => wp.outputs
  | .differentOutputsSameTracks       =>
      wp.outputs.filter (· == wp.switchState.currentValue)
  | .differentOutputsSameTracksSplitAV =>
      match pkt.mediaType with
      | .video    => wp.videoOutputs.filter (· == wp.switchState.currentValue)
      | .audio    => wp.audioOutputs.filter (· == wp.switchState.currentValue)
      | .subtitle => wp.audioOutputs.filter (· == wp.switchState.currentValue)
      | .data     => wp.audioOutputs.filter (· == wp.switchState.currentValue)

/-! ## Well-formedness

A `WiredPipeline` is well-formed when its switch state is consistent with
the output topology. These preconditions are required for the routing
correctness theorems. -/

/-- A wired pipeline is well-formed when:
    1. The current switch value is one of the declared outputs.
    2. No pending switch (stable state).
    3. For SplitAV mode, audio and video outputs are disjoint. -/
structure WiredPipeline.WellFormed (wp : WiredPipeline) : Prop where
  currentInOutputs : wp.switchState.currentValue ∈ wp.outputs
  stable           : wp.switchState.nextValue = none
  splitAVDisjoint  : wp.mode = .differentOutputsSameTracksSplitAV →
                     ∀ id, ¬(id ∈ wp.audioOutputs ∧ id ∈ wp.videoOutputs)

end Pipeline
