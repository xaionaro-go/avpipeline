-- Proofs/ReorderDTS.lean: Correctness proofs for DTS reordering kernel

import Spec.ReorderDTS

open ReorderState

/-! ## Helper lemmas about sorted insert -/

theorem sortedInsert_nonempty (x : Int) (xs : List Int) :
    (sortedInsert x xs) ≠ [] := by
  induction xs with
  | nil => simp [sortedInsert]
  | cons h t ih =>
    simp only [sortedInsert]
    split <;> simp

theorem sortedInsertItem_nonempty (item : Item) (xs : List Item) :
    (sortedInsertItem item xs) ≠ [] := by
  induction xs with
  | nil => simp [sortedInsertItem]
  | cons h t ih =>
    simp only [sortedInsertItem]
    split <;> simp

theorem sortedInsert_length (x : Int) (xs : List Int) :
    (sortedInsert x xs).length = xs.length + 1 := by
  induction xs with
  | nil => simp [sortedInsert]
  | cons h t ih =>
    simp only [sortedInsert]
    split
    · simp
    · simp [ih]

theorem sortedInsertItem_length (item : Item) (xs : List Item) :
    (sortedInsertItem item xs).length = xs.length + 1 := by
  induction xs with
  | nil => simp [sortedInsertItem]
  | cons h t ih =>
    simp only [sortedInsertItem]
    split
    · simp
    · simp [ih]

theorem sortedInsert_mem (x y : Int) (xs : List Int) :
    y ∈ sortedInsert x xs ↔ y = x ∨ y ∈ xs := by
  induction xs with
  | nil => simp [sortedInsert]
  | cons h t ih =>
    simp only [sortedInsert]
    split
    · simp [List.mem_cons]
    · constructor
      · intro hm
        rw [List.mem_cons] at hm
        rcases hm with rfl | hm
        · right; exact List.Mem.head _
        · rw [ih] at hm
          rcases hm with rfl | hm
          · left; rfl
          · right; exact List.Mem.tail _ hm
      · intro hm
        rw [List.mem_cons]
        rcases hm with rfl | hm
        · right; rw [ih]; left; rfl
        · rw [List.mem_cons] at hm
          rcases hm with rfl | hm
          · left; rfl
          · right; rw [ih]; right; exact hm

theorem sortedInsertItem_mem (item x : Item) (xs : List Item) :
    x ∈ sortedInsertItem item xs ↔ x = item ∨ x ∈ xs := by
  induction xs with
  | nil => simp [sortedInsertItem]
  | cons h t ih =>
    simp only [sortedInsertItem]
    split
    · simp [List.mem_cons]
    · constructor
      · intro hm
        rw [List.mem_cons] at hm
        rcases hm with rfl | hm
        · right; exact List.Mem.head _
        · rw [ih] at hm
          rcases hm with rfl | hm
          · left; rfl
          · right; exact List.Mem.tail _ hm
      · intro hm
        rw [List.mem_cons]
        rcases hm with rfl | hm
        · right; rw [ih]; left; rfl
        · rw [List.mem_cons] at hm
          rcases hm with rfl | hm
          · left; rfl
          · right; rw [ih]; right; exact hm

/-! ## Sorted insert preserves sortedness -/

private theorem isSorted_cons_sortedInsert (a x : Int) (rest : List Int)
    (hax : a < x) (hSorted : IsSorted (a :: rest)) :
    IsSorted (a :: sortedInsert x rest) := by
  cases rest with
  | nil =>
    simp [sortedInsert, IsSorted]
    omega
  | cons b rest' =>
    simp only [sortedInsert]
    split
    case isTrue hle =>
      exact ⟨Int.le_of_lt hax, ⟨hle, hSorted.2⟩⟩
    case isFalse hgt =>
      have hab : a ≤ b := hSorted.1
      have hSorted_tail : IsSorted (b :: rest') := hSorted.2
      have : IsSorted (b :: sortedInsert x rest') :=
        isSorted_cons_sortedInsert b x rest' (by omega) hSorted_tail
      exact ⟨hab, this⟩
  termination_by rest.length

theorem sortedInsert_preserves_sorted (x : Int) (xs : List Int) (h : IsSorted xs) :
    IsSorted (sortedInsert x xs) := by
  cases xs with
  | nil => simp [sortedInsert, IsSorted]
  | cons a rest =>
    simp only [sortedInsert]
    split
    case isTrue hle =>
      cases rest with
      | nil => exact ⟨hle, trivial⟩
      | cons b rest' => exact ⟨hle, h⟩
    case isFalse hgt =>
      exact isSorted_cons_sortedInsert a x rest (by omega) h

private theorem isSortedItems_cons_sortedInsertItem (a item : Item) (rest : List Item)
    (hax : a.dts < item.dts) (hSorted : IsSortedItems (a :: rest)) :
    IsSortedItems (a :: sortedInsertItem item rest) := by
  cases rest with
  | nil =>
    simp [sortedInsertItem, IsSortedItems]
    omega
  | cons b rest' =>
    simp only [sortedInsertItem]
    split
    case isTrue hle =>
      exact ⟨Int.le_of_lt hax, ⟨hle, hSorted.2⟩⟩
    case isFalse hgt =>
      have hab : a.dts ≤ b.dts := hSorted.1
      have hSorted_tail : IsSortedItems (b :: rest') := hSorted.2
      have : IsSortedItems (b :: sortedInsertItem item rest') :=
        isSortedItems_cons_sortedInsertItem b item rest' (by omega) hSorted_tail
      exact ⟨hab, this⟩
  termination_by rest.length

theorem sortedInsertItem_preserves_sorted (item : Item) (xs : List Item)
    (h : IsSortedItems xs) : IsSortedItems (sortedInsertItem item xs) := by
  cases xs with
  | nil => simp [sortedInsertItem, IsSortedItems]
  | cons a rest =>
    simp only [sortedInsertItem]
    split
    case isTrue hle =>
      cases rest with
      | nil => exact ⟨hle, trivial⟩
      | cons b rest' => exact ⟨hle, h⟩
    case isFalse hgt =>
      exact isSortedItems_cons_sortedInsertItem a item rest (by omega) h

/-! ## Property 1: Output DTS monotonicity via doSendItem

  The key property: doSendItem only emits items with DTS >= prevDTS.
  This directly models the Go check at line 287: `if r.PrevDTS > dts`.
-/

/-- doSendItem: when an item is successfully sent, its DTS >= prevDTS. -/
theorem doSendItem_monotonic (s : ReorderState) (dts : Int)
    (h : (s.doSendItem dts).1 = SendResult.sent dts) :
    s.prevDTS ≤ dts := by
  simp only [doSendItem] at h
  split at h
  · split at h <;> simp at h
  · omega

/-- doSendItem: successfully sent item updates prevDTS to that DTS. -/
theorem doSendItem_updates_prevDTS (s : ReorderState) (dts : Int)
    (h : ¬(s.prevDTS > dts)) :
    (s.doSendItem dts).2.prevDTS = dts := by
  unfold doSendItem
  split
  · omega
  · rfl

/-- doSendItem: new prevDTS >= old prevDTS (monotonically non-decreasing). -/
theorem doSendItem_prevDTS_nondecreasing (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.prevDTS ≥ s.prevDTS := by
  unfold doSendItem
  split
  · split <;> simp
  · simp; omega

/-- doSendItem: sent DTS is prepended to emitted list. -/
theorem doSendItem_emits (s : ReorderState) (dts : Int)
    (h : ¬(s.prevDTS > dts)) :
    (s.doSendItem dts).2.emitted = dts :: s.emitted := by
  unfold doSendItem
  split
  · omega
  · rfl

/-- doSendItem: when prevDTS > dts, state is unchanged. -/
theorem doSendItem_no_change_on_fail (s : ReorderState) (dts : Int)
    (h : s.prevDTS > dts) :
    (s.doSendItem dts).2 = s := by
  unfold doSendItem
  split
  · split <;> rfl
  · omega

/-! ## Property 2: emptyQueuesCount tracking -/

/-- sendOneItemFromQueue on empty queue returns none. -/
theorem sendOneItemFromQueue_empty (s : ReorderState)
    (h : s.globalQueue = []) :
    s.sendOneItemFromQueue = none := by
  simp [sendOneItemFromQueue, h]

/-- Initial state has EmptyCountCorrect. -/
theorem init_empty_count_correct (maxDiff cap : Nat) (discard : Bool) :
    EmptyCountCorrect (ReorderState.init maxDiff cap discard) := by
  simp [EmptyCountCorrect, ReorderState.countEmpty, ReorderState.init]

/-! ## Property 3: Items emitted only when emptyQueuesCount = 0

  pullAndSendPendingItems is a no-op when emptyQueuesCount != 0.
  This is the guard that ensures items are only emitted when all
  streams have data in their queues.
-/

/-- pullAndSendPendingItems does nothing when emptyQueuesCount != 0. -/
theorem pullAndSend_requires_all_nonempty (s : ReorderState) (fuel : Nat)
    (h : s.emptyQueuesCount ≠ 0) :
    pullAndSendPendingItems s fuel = s := by
  cases fuel with
  | zero => simp [pullAndSendPendingItems]
  | succ n => simp [pullAndSendPendingItems, h]

/-! ## Property 4: enforceLowDTSDifference discards too-old items

  Go code line 203-205: when newItemDTS < currentDTS - maxDTSDifference,
  the item is discarded.
-/

/-- Items with DTS far below current minimum are discarded. -/
theorem enforce_discards_too_old (s : ReorderState) (newDTS : Int) (curDTS : Int)
    (hCur : s.currentDTS = some curDTS)
    (hTooOld : curDTS - newDTS > s.maxDTSDiff) :
    s.enforceLowDTSDifference newDTS = EnforceResult.discard := by
  unfold enforceLowDTSDifference
  rw [hCur]
  have : -(newDTS - curDTS) > ↑s.maxDTSDiff := by omega
  simp [this]

/-! ## Property 5: enforceLowDTSDifference flushes when item too new

  Go code line 206-221: when newItemDTS > currentDTS + maxDTSDifference,
  old items are flushed until the gap is acceptable.
-/

/-- Items with DTS far above current minimum trigger a flush. -/
theorem enforce_flushes_too_new (s : ReorderState) (newDTS : Int) (curDTS : Int)
    (hCur : s.currentDTS = some curDTS)
    (hNotTooOld : ¬(curDTS - newDTS > ↑s.maxDTSDiff))
    (hTooNew : newDTS - curDTS > s.maxDTSDiff) :
    ∃ s', s.enforceLowDTSDifference newDTS = EnforceResult.continue_ s' ∧
      s' = flushUntilClose s newDTS s.globalQueue.length := by
  refine ⟨flushUntilClose s newDTS s.globalQueue.length, ?_, rfl⟩
  unfold enforceLowDTSDifference
  rw [hCur]
  have h1 : ¬(-(newDTS - curDTS) > ↑s.maxDTSDiff) := by omega
  simp [h1, hTooNew]

/-! ## Property 6: pullAndSendPendingItems terminates -/

/-- With zero fuel, pullAndSendPendingItems is identity. -/
theorem pullAndSend_zero_fuel (s : ReorderState) :
    pullAndSendPendingItems s 0 = s := by
  simp [pullAndSendPendingItems]

/-! ## Property 7: doSendItem preserves queue structure -/

/-- doSendItem does not change the global queue. -/
theorem doSendItem_preserves_queue (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.globalQueue = s.globalQueue := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves stream queues. -/
theorem doSendItem_preserves_streamQueues (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.streamQueues = s.streamQueues := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves knownStreams. -/
theorem doSendItem_preserves_knownStreams (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.knownStreams = s.knownStreams := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves emptyQueuesCount. -/
theorem doSendItem_preserves_eqc (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.emptyQueuesCount = s.emptyQueuesCount := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves capacity. -/
theorem doSendItem_preserves_capacity (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.capacity = s.capacity := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves maxDTSDiff. -/
theorem doSendItem_preserves_maxDTSDiff (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.maxDTSDiff = s.maxDTSDiff := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-- doSendItem preserves discardMode. -/
theorem doSendItem_preserves_discardMode (s : ReorderState) (dts : Int) :
    (s.doSendItem dts).2.discardMode = s.discardMode := by
  unfold doSendItem; split
  · split <;> rfl
  · rfl

/-! ## Property 8: Global queue minimum = minimum across all stream queue minimums

  When the global queue is sorted, the head is the minimum.
-/

/-- For an empty global queue, currentDTS is none. -/
theorem currentDTS_empty (s : ReorderState) (h : s.globalQueue = []) :
    s.currentDTS = none := by
  simp [currentDTS, h]

/-- For a nonempty global queue, currentDTS is the first element's DTS. -/
theorem currentDTS_nonempty (s : ReorderState) (item : Item) (rest : List Item)
    (h : s.globalQueue = item :: rest) :
    s.currentDTS = some item.dts := by
  simp [currentDTS, h]

/-- In a sorted item list, the head DTS is <= all other DTS values. -/
theorem sorted_head_is_min (item : Item) (rest : List Item)
    (h : IsSortedItems (item :: rest)) :
    ∀ x ∈ rest, item.dts ≤ x.dts := by
  cases rest with
  | nil => intro x hx; exact absurd hx (List.not_mem_nil x)
  | cons b rest' =>
    intro x hx
    have hab : item.dts ≤ b.dts := h.1
    have hSorted_b : IsSortedItems (b :: rest') := h.2
    cases hx with
    | head => exact hab
    | tail _ hx' =>
      have hb_le_x : b.dts ≤ x.dts := sorted_head_is_min b rest' hSorted_b x hx'
      omega
  termination_by rest.length

/-- The global queue's currentDTS is the minimum DTS among all items in the queue,
    provided the queue is sorted. -/
theorem currentDTS_is_global_min (s : ReorderState) (item : Item) (rest : List Item)
    (hq : s.globalQueue = item :: rest)
    (hSorted : IsSortedItems s.globalQueue) :
    ∀ x ∈ s.globalQueue, item.dts ≤ x.dts := by
  rw [hq]
  intro x hx
  rw [List.mem_cons] at hx
  rcases hx with rfl | hx
  · omega
  · rw [hq] at hSorted
    exact sorted_head_is_min item rest hSorted x hx

/-! ## Enforce and flush structural properties -/

/-- enforceLowDTSDifference with empty queue always continues with unchanged state. -/
theorem enforce_empty_continues (s : ReorderState) (newDTS : Int)
    (h : s.globalQueue = []) :
    s.enforceLowDTSDifference newDTS = EnforceResult.continue_ s := by
  simp [enforceLowDTSDifference, currentDTS, h]

/-- enforceLowDTSDifference with DTS in range continues with same state. -/
theorem enforce_in_range_continues (s : ReorderState) (newDTS : Int) (curDTS : Int)
    (hCur : s.currentDTS = some curDTS)
    (hNotTooOld : ¬(curDTS - newDTS > ↑s.maxDTSDiff))
    (hNotTooNew : ¬(newDTS - curDTS > ↑s.maxDTSDiff)) :
    s.enforceLowDTSDifference newDTS = EnforceResult.continue_ s := by
  unfold enforceLowDTSDifference
  rw [hCur]
  have h1 : ¬(-(newDTS - curDTS) > ↑s.maxDTSDiff) := by omega
  simp [h1, hNotTooNew]

/-! ## Initial state properties -/

/-- Initial state has empty queue. -/
theorem init_empty (maxDiff cap : Nat) (discard : Bool) :
    (ReorderState.init maxDiff cap discard).globalQueue = [] := by
  simp [ReorderState.init]

/-- Initial state has emptyQueuesCount = 0. -/
theorem init_eqc_zero (maxDiff cap : Nat) (discard : Bool) :
    (ReorderState.init maxDiff cap discard).emptyQueuesCount = 0 := by
  simp [ReorderState.init]

/-- Initial state has no known streams. -/
theorem init_no_streams (maxDiff cap : Nat) (discard : Bool) :
    (ReorderState.init maxDiff cap discard).knownStreams = [] := by
  simp [ReorderState.init]

/-- The initial state satisfies OutputMonotonic (empty emitted list). -/
theorem init_output_monotonic (maxDiff cap : Nat) (discard : Bool) :
    OutputMonotonic (ReorderState.init maxDiff cap discard) := by
  simp [OutputMonotonic, ReorderState.init, IsSorted]

/-! ## flushUntilClose properties -/

/-- flushUntilClose with zero fuel is identity. -/
theorem flushUntilClose_zero_fuel (s : ReorderState) (targetDTS : Int) :
    flushUntilClose s targetDTS 0 = s := by
  simp [flushUntilClose]

/-- flushUntilClose with empty queue is identity. -/
theorem flushUntilClose_empty (s : ReorderState) (targetDTS : Int) (fuel : Nat)
    (h : s.globalQueue = []) :
    flushUntilClose s targetDTS (fuel + 1) = s := by
  simp [flushUntilClose, currentDTS, h]

/-- flushUntilClose when already in range is identity. -/
theorem flushUntilClose_in_range (s : ReorderState) (targetDTS : Int) (fuel : Nat)
    (item : Item) (rest : List Item)
    (hq : s.globalQueue = item :: rest)
    (hRange : item.dts + s.maxDTSDiff ≥ targetDTS) :
    flushUntilClose s targetDTS (fuel + 1) = s := by
  simp [flushUntilClose, currentDTS, hq, hRange]
