-- Spec/Pipeline/OutputChain.lean: Output chain model for packet routing.
-- Models the sequence of processing steps a packet traverses from an
-- input node to an output sender, corresponding to the chain of
-- AddPushTo calls with filter conditions and transforms in Go.

import Spec.Pipeline.Types
import Spec.Pipeline.FilterCondition

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Chain step

A single processing step in an output chain. Each step has a name
(for tracing), a gate condition (packet is dropped if it does not
match), and a transform function applied to packets that pass the
gate. Corresponds to one `AddPushTo` call with its filter closure
and any packet transformation the target node performs. -/
structure ChainStep where
  name      : String
  condition : FilterCondition
  transform : PipePacket → PipePacket

/-! ## Output chain

An ordered list of chain steps that a packet must traverse to reach
an output. Corresponds to the sequence of nodes and filters between
an input node and an output sender in the streammux pipeline. -/
abbrev OutputChain := List ChainStep

/-! ## Step application

Apply a single chain step to a packet: if the gate condition matches,
transform the packet and return it; otherwise the packet is dropped. -/
def ChainStep.apply (step : ChainStep) (pkt : PipePacket) : Option PipePacket :=
  if step.condition.match pkt then
    some (step.transform pkt)
  else
    none

/-! ## Chain processing

Process a packet through an entire output chain by folding left with
monadic bind: each step receives the transformed packet from the
previous step, and any step may drop it. -/
def OutputChain.process (chain : OutputChain) (pkt : PipePacket) : Option PipePacket :=
  chain.foldlM (fun pkt step => step.apply pkt) pkt

/-! ## Standard step constructors

Convenience constructors for common step patterns. -/

/-- An identity step: always passes, never transforms. Useful as a
    placeholder or for naming a passthrough node. -/
def identityStep (name : String) : ChainStep :=
  { name, condition := .always, transform := id }

/-- A gate step: filters packets by a condition but does not transform
    them. Corresponds to an `AddPushTo` with a filter and no processing. -/
def gateStep (name : String) (cond : FilterCondition) : ChainStep :=
  { name, condition := cond, transform := id }

/-- A transform step: always passes but applies a transformation.
    Corresponds to a processing node with no filter condition. -/
def transformStep (name : String) (f : PipePacket → PipePacket) : ChainStep :=
  { name, condition := .always, transform := f }

end Pipeline
