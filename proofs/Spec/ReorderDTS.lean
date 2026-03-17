-- Spec/ReorderDTS.lean: Formal specification of DTS reordering kernel from
-- kernel/reorder_monotonic_dts.go

/-!
  Models the ReorderMonotonicDTS kernel which reorders packets/frames across
  multiple streams to ensure globally monotonic DTS output.

  Key abstraction: heaps are modeled as sorted lists (ascending by DTS).
  Stream queues are modeled as a function from stream index to sorted list.
  The global queue is the merge of all stream queues.

  We model DTS values as Int (matching Go's int64).
-/

/-- A stream key, abstracting InternalStreamKey. -/
abbrev StreamKey := Nat

/-- An item in the queue, carrying a DTS value and stream key. -/
structure Item where
  dts : Int
  streamKey : StreamKey
  deriving Repr, DecidableEq

/-- Sorted insert into an ascending list. -/
def sortedInsert (x : Int) : List Int → List Int
  | [] => [x]
  | h :: t => if x ≤ h then x :: h :: t else h :: sortedInsert x t

/-- A sorted list (ascending, allowing duplicates). -/
def IsSorted : List Int → Prop
  | [] => True
  | [_] => True
  | a :: b :: rest => a ≤ b ∧ IsSorted (b :: rest)

/-- Sorted insert into an ascending item list (by DTS). -/
def sortedInsertItem (item : Item) : List Item → List Item
  | [] => [item]
  | h :: t => if item.dts ≤ h.dts then item :: h :: t else h :: sortedInsertItem item t

/-- Items sorted by DTS (ascending). -/
def IsSortedItems : List Item → Prop
  | [] => True
  | [_] => True
  | a :: b :: rest => a.dts ≤ b.dts ∧ IsSortedItems (b :: rest)

/-- The reorder kernel state. -/
structure ReorderState where
  /-- Global priority queue of items, sorted ascending by DTS. -/
  globalQueue : List Item
  /-- Per-stream queues, each sorted ascending by DTS. -/
  streamQueues : StreamKey → List Int
  /-- Set of known streams. -/
  knownStreams : List StreamKey
  /-- Count of streams with empty queues. -/
  emptyQueuesCount : Nat
  /-- Previous DTS of last emitted item. -/
  prevDTS : Int
  /-- Maximum allowed DTS difference. -/
  maxDTSDiff : Nat
  /-- Maximum queue capacity. -/
  capacity : Nat
  /-- Whether to discard (vs send) when queue full or DTS goes backward. -/
  discardMode : Bool
  /-- Items emitted so far (most recent first). -/
  emitted : List Int

namespace ReorderState

/-- Initial state. -/
def init (maxDiff cap : Nat) (discard : Bool) : ReorderState :=
  { globalQueue := []
    streamQueues := fun _ => []
    knownStreams := []
    emptyQueuesCount := 0
    prevDTS := -1
    maxDTSDiff := maxDiff
    capacity := cap
    discardMode := discard
    emitted := [] }

/-- Current minimum DTS in the global queue (None if empty). -/
def currentDTS (s : ReorderState) : Option Int :=
  match s.globalQueue with
  | [] => none
  | h :: _ => some h.dts

/-- Count empty queues from the known streams list (reference computation). -/
def countEmpty (s : ReorderState) : Nat :=
  (s.knownStreams.filter (fun k => (s.streamQueues k).isEmpty)).length

/-!
  ## doSendItem: emit one item checking monotonicity.

  Go code (lines 277-304): checks prevDTS <= dts, updates prevDTS.
  In discard mode, backward-DTS items are silently discarded.
  In non-discard mode, it returns an error.
-/

/-- Result of attempting to send an item. -/
inductive SendResult where
  | sent (dts : Int)
  | discarded
  | errBackward
  deriving Repr, DecidableEq

/-- Attempt to emit an item: check DTS monotonicity. -/
def doSendItem (s : ReorderState) (dts : Int) : SendResult × ReorderState :=
  if s.prevDTS > dts then
    if s.discardMode then
      (SendResult.discarded, s)
    else
      (SendResult.errBackward, s)
  else
    (SendResult.sent dts, { s with prevDTS := dts, emitted := dts :: s.emitted })

/-!
  ## sendOneItemFromQueue: pop the global minimum, update stream queue,
  update emptyQueuesCount, call doSendItem.
-/

/-- Pop the minimum item from the global queue and its stream queue.
    Returns the send result and updated state. -/
def sendOneItemFromQueue (s : ReorderState) : Option (SendResult × ReorderState) :=
  match s.globalQueue with
  | [] => none
  | item :: rest =>
    let streamQ := s.streamQueues item.streamKey
    let newStreamQ := streamQ.tail
    let nowEmpty := newStreamQ.isEmpty
    let newEqc := if nowEmpty && !streamQ.isEmpty then s.emptyQueuesCount + 1
                  else s.emptyQueuesCount
    let s' : ReorderState :=
      { s with
        globalQueue := rest
        streamQueues := fun k =>
          if k == item.streamKey then newStreamQ else s.streamQueues k
        emptyQueuesCount := newEqc }
    some (s'.doSendItem item.dts)

/-!
  ## enforceLowDTSDifference

  Go code (lines 190-225):
  - If queue empty, always continue.
  - If newDTS < currentMin - maxDiff: discard (return false).
  - If newDTS > currentMin + maxDiff: flush old items until within range.
-/

/-- Result of enforce check. -/
inductive EnforceResult where
  | continue_ (s : ReorderState)
  | discard

/-- Flush old items until currentDTS + maxDiff >= targetDTS or queue empty. -/
def flushUntilClose (s : ReorderState) (targetDTS : Int) (fuel : Nat) : ReorderState :=
  match fuel with
  | 0 => s
  | fuel' + 1 =>
    match s.currentDTS with
    | none => s
    | some curDTS =>
      if curDTS + s.maxDTSDiff ≥ targetDTS then s
      else
        match s.sendOneItemFromQueue with
        | none => s
        | some (_, s') => flushUntilClose s' targetDTS fuel'

/-- Enforce low DTS difference for a new item. -/
def enforceLowDTSDifference (s : ReorderState) (newDTS : Int) : EnforceResult :=
  match s.currentDTS with
  | none => EnforceResult.continue_ s
  | some curDTS =>
    let diff := newDTS - curDTS
    if -diff > s.maxDTSDiff then
      EnforceResult.discard
    else if diff > s.maxDTSDiff then
      let s' := flushUntilClose s newDTS s.globalQueue.length
      EnforceResult.continue_ s'
    else
      EnforceResult.continue_ s

/-!
  ## pullAndSendPendingItems

  Go code (lines 260-275): loop while emptyQueuesCount == 0.
-/

/-- Pull and send items while all queues have data. -/
def pullAndSendPendingItems (s : ReorderState) (fuel : Nat) : ReorderState :=
  match fuel with
  | 0 => s
  | fuel' + 1 =>
    if s.emptyQueuesCount ≠ 0 then s
    else
      match s.sendOneItemFromQueue with
      | none => s
      | some (_, s') => pullAndSendPendingItems s' fuel'

/-!
  ## pushToQueue: the main entry point.

  Go code (lines 112-182):
  1. enforceLowDTSDifference
  2. If queue full: pop oldest (discard mode) or send oldest (non-discard mode)
  3. Push item to global queue and stream queue
  4. Update emptyQueuesCount
  5. When emptyQueuesCount = 0: pullAndSendPendingItems
-/

/-- Push an item to the queue. Returns updated state. -/
def pushToQueue (s : ReorderState) (item : Item) : ReorderState :=
  match s.enforceLowDTSDifference item.dts with
  | EnforceResult.discard => s
  | EnforceResult.continue_ s₁ =>
    -- Handle full queue
    let s₂ :=
      if s₁.globalQueue.length ≥ s₁.capacity then
        if s₁.discardMode then
          match s₁.globalQueue with
          | [] => s₁
          | oldest :: rest =>
            let sq := s₁.streamQueues oldest.streamKey
            let newSq := sq.tail
            { s₁ with
              globalQueue := rest
              streamQueues := fun k =>
                if k == oldest.streamKey then newSq else s₁.streamQueues k
              emptyQueuesCount :=
                if newSq.isEmpty && !sq.isEmpty then s₁.emptyQueuesCount + 1
                else s₁.emptyQueuesCount }
        else
          match s₁.sendOneItemFromQueue with
          | none => s₁
          | some (_, s') => s'
      else s₁
    -- Push item to both queues
    let newGlobal := sortedInsertItem item s₂.globalQueue
    let streamQ := s₂.streamQueues item.streamKey
    let newStreamQ := sortedInsert item.dts streamQ
    let isNewStream := !(s₂.knownStreams.contains item.streamKey)
    let newKnown :=
      if isNewStream then item.streamKey :: s₂.knownStreams else s₂.knownStreams
    let eqcNew :=
      if isNewStream then s₂.emptyQueuesCount
      else if streamQ.isEmpty then s₂.emptyQueuesCount - 1
      else s₂.emptyQueuesCount
    let s₃ : ReorderState :=
      { s₂ with
        globalQueue := newGlobal
        streamQueues := fun k =>
          if k == item.streamKey then newStreamQ else s₂.streamQueues k
        knownStreams := newKnown
        emptyQueuesCount := eqcNew }
    if s₃.emptyQueuesCount == 0 then
      pullAndSendPendingItems s₃ s₃.globalQueue.length
    else
      s₃

end ReorderState

/-! ## Well-formedness invariants -/

/-- The emptyQueuesCount tracking invariant. -/
def EmptyCountCorrect (s : ReorderState) : Prop :=
  s.emptyQueuesCount = s.countEmpty

/-- Output monotonicity: emitted items in reverse order are non-decreasing.
    Since emitted is most-recent-first, this means each new emission has
    DTS >= all previous emissions. -/
def OutputMonotonic (s : ReorderState) : Prop :=
  IsSorted s.emitted.reverse
