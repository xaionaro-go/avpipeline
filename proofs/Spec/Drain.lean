-- Spec/Drain.lean: Formal specification of drain protocol from drain.go

/-- State of a single pipeline node relevant to drain operations. -/
structure NodeState where
  flushed : Bool
  drained : Bool
  blocked : Bool
  deriving Repr, DecidableEq

namespace Drain

/-! ## Per-node operations

These model the per-node callbacks invoked by Traverse in drain.go.
-/

/-- Flush a node: sets flushed=true. Models `n.Flush(ctx)`. -/
def flushNode (n : NodeState) : NodeState :=
  { n with flushed := true }

/-- Block input on a node: sets blocked to the given value. Models `node.SetBlockInput`. -/
def setBlockInputNode (b : Bool) (n : NodeState) : NodeState :=
  { n with blocked := b }

/--
  Drain a single node with optional block-input.
  Models the per-node callback in `Drain()` from drain.go lines 26-47.
  After draining, the node is flushed and drained (the poll loop waits until drained).
-/
def drainNode (setBlock : Option Bool) (n : NodeState) : NodeState :=
  let n₁ := match setBlock with
    | some b => setBlockInputNode b n
    | none   => n
  let n₂ := flushNode n₁
  -- The poll loop waits until IsDrained returns true, so post-drain the node is drained
  { n₂ with drained := true }

/-! ## Operations over node lists

These model the Traverse-based functions from drain.go.
Traverse visits each node exactly once, applying the callback.
We model this as `List.map` over the flat node list.
-/

/--
  `IsDrained` from drain.go: returns true iff every node is drained.
  The Go code uses Traverse with early-stop on the first non-drained node,
  but the observable result is: all drained ↔ true.
-/
def isDrained (nodes : List NodeState) : Bool :=
  nodes.all (·.drained)

/--
  `SetBlockInput` from drain.go: sets blocked on every node via Traverse.
-/
def setBlockInput (b : Bool) (nodes : List NodeState) : List NodeState :=
  nodes.map (setBlockInputNode b)

/--
  `Drain` from drain.go: for each node (via Traverse), optionally block input,
  flush, then poll until drained.
-/
def drain (setBlock : Option Bool) (nodes : List NodeState) : List NodeState :=
  nodes.map (drainNode setBlock)

end Drain
