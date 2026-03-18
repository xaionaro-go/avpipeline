-- Spec/Pipeline/Barrier.lean: Barrier/switch model for packet routing.
-- Models the OutputSwitch and OutputSyncer from Go's
-- kernel/barrier/stategetter/switch.go, which gate packets based on
-- which output is currently active.

import Spec.Pipeline.Types
import Spec.Pipeline.FilterCondition

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Barrier decision

The three possible outcomes when a barrier examines a packet.
Corresponds to `types.State` in Go (`StatePass`, `StateDrop`, `StateBlock`). -/
inductive BarrierDecision where
  | pass
  | drop
  | block
  deriving DecidableEq, Repr

/-! ## Switch state

Models the state of a `stategetter.Switch` instance. The switch tracks
which output is currently active (`currentValue`), whether a pending
switch has been requested (`nextValue`), and the previous output for
transition logic (`previousValue`). The `keepUnless` condition determines
when the pending switch actually commits (e.g., on a keyframe). -/
structure SwitchState where
  currentValue  : PipeOutputID
  nextValue     : Option PipeOutputID
  previousValue : PipeOutputID
  keepUnless    : FilterCondition
  deriving Repr

/-! ## Output switch logic

Models `SwitchOutput.GetState` from Go, which determines whether to pass,
drop, or block a packet for a given output, and potentially commits a
pending value switch (CAS logic).

The logic:
1. No pending switch (`nextValue = none`): pass if current output matches,
   else drop.
2. Pending switch to same value (`next == currentValue`): treat as no pending.
3. Pending switch to different value and `keepUnless` matches the packet:
   commit the switch (update currentValue, clear nextValue, record
   previousValue), then pass/drop based on the new currentValue.
4. Pending switch but `keepUnless` does not match: pass if current output
   matches, else drop (switch stays pending). -/
def outputSwitchGetState
    (sw : SwitchState) (outputID : PipeOutputID) (pkt : PipePacket)
    : SwitchState × BarrierDecision :=
  match sw.nextValue with
  | none =>
    -- No pending switch: simple current-value check.
    if sw.currentValue == outputID then (sw, .pass) else (sw, .drop)
  | some next =>
    if next == sw.currentValue then
      -- Pending value equals current: same as no pending switch.
      if sw.currentValue == outputID then (sw, .pass) else (sw, .drop)
    else if sw.keepUnless.match pkt then
      -- Commit the switch: CAS currentValue ← next, clear nextValue.
      let sw' : SwitchState := {
        currentValue  := next
        nextValue     := none
        previousValue := sw.currentValue
        keepUnless    := sw.keepUnless
      }
      if next == outputID then (sw', .pass) else (sw', .drop)
    else
      -- keepUnless did not match: hold the pending switch.
      if sw.currentValue == outputID then (sw, .pass) else (sw, .drop)

/-! ## Output syncer logic

Models the syncer barrier, which uses a `Switch` with the `InactiveBlock`
flag. The syncer blocks all outputs that are not the currently active one,
ensuring that only the active output receives packets. Unlike the switch,
the syncer never commits a pending value on its own — it is driven by the
switch's `OnAfterSwitch` callback.

For the purposes of the routing proof, the syncer's `GetState` reduces to:
pass if the output is current, block otherwise. -/
def outputSyncerGetState
    (sw : SwitchState) (outputID : PipeOutputID)
    : BarrierDecision :=
  if sw.currentValue == outputID then .pass else .block

/-! ## Channel

A bounded FIFO buffer modeling the channel between pipeline nodes.
Packets queue up when the downstream node is blocked (e.g., by the
syncer barrier) and are released when the barrier opens. -/
structure Channel where
  capacity : Nat
  buffer   : List PipePacket
  deriving Repr

/-- Push a packet into the channel. Returns `none` if the channel is
    at capacity (back-pressure). -/
def Channel.push (ch : Channel) (pkt : PipePacket) : Option Channel :=
  if ch.buffer.length < ch.capacity then
    some { capacity := ch.capacity, buffer := ch.buffer ++ [pkt] }
  else
    none

/-- Pop the oldest packet from the channel. Returns `none` if the
    channel is empty. -/
def Channel.pop (ch : Channel) : Option (PipePacket × Channel) :=
  match ch.buffer with
  | []      => none
  | p :: ps => some (p, { capacity := ch.capacity, buffer := ps })

/-! ## Full barrier check

Combines the output switch and output syncer into a single decision
function. This models the two-barrier chain that every packet in the
streammux must pass through: first the switch decides whether the
packet should reach this output at all, then the syncer decides
whether the output is ready to receive it.

The switch may update its state (committing a pending value), so its
updated state is threaded through. The syncer state is read-only here
(it is updated externally by the switch's `OnAfterSwitch` callback). -/
def fullBarrierCheck
    (switchSt : SwitchState) (syncerSt : SwitchState)
    (outputID : PipeOutputID) (pkt : PipePacket)
    : SwitchState × BarrierDecision :=
  let (switchSt', switchDecision) := outputSwitchGetState switchSt outputID pkt
  match switchDecision with
  | .drop  => (switchSt', .drop)
  | .block => (switchSt', .block)
  | .pass  =>
    let syncerDecision := outputSyncerGetState syncerSt outputID
    (switchSt', syncerDecision)

end Pipeline
