-- Proofs/Drain.lean: Correctness proofs for the drain protocol

import Spec.Drain

open Drain

/-! ## IsDrained: empty list -/

/-- IsDrained on an empty list returns true. -/
theorem isDrained_empty : isDrained [] = true := by
  simp [isDrained]

/-! ## IsDrained: false if any node not drained -/

/-- If any node in the list is not drained, IsDrained returns false. -/
theorem isDrained_false_of_exists_not_drained (nodes : List NodeState)
    (h : ∃ n, n ∈ nodes ∧ n.drained = false) :
    isDrained nodes = false := by
  simp [isDrained, List.all_eq_true]
  obtain ⟨n, hn_mem, hn_not⟩ := h
  exact ⟨n, hn_mem, by simp [hn_not]⟩

/-! ## IsDrained: true iff all drained -/

/-- IsDrained is true if and only if every node is drained. -/
theorem isDrained_iff_all_drained (nodes : List NodeState) :
    isDrained nodes = true ↔ ∀ n, n ∈ nodes → n.drained = true := by
  simp [isDrained, List.all_eq_true]

/-! ## SetBlockInput sets blocked=true on all nodes -/

/-- After SetBlockInput true, every node has blocked=true. -/
theorem setBlockInput_true_all_blocked (nodes : List NodeState) :
    ∀ n, n ∈ setBlockInput true nodes → n.blocked = true := by
  intro n hn
  simp [setBlockInput, List.mem_map] at hn
  obtain ⟨m, _, rfl⟩ := hn
  simp [setBlockInputNode]

/-- SetBlockInput preserves list length. -/
theorem setBlockInput_length (b : Bool) (nodes : List NodeState) :
    (setBlockInput b nodes).length = nodes.length := by
  simp [setBlockInput]

/-! ## After Drain, all nodes are flushed -/

/-- After Drain, every node has flushed=true. -/
theorem drain_all_flushed (setBlock : Option Bool) (nodes : List NodeState) :
    ∀ n, n ∈ drain setBlock nodes → n.flushed = true := by
  intro n hn
  simp [drain, List.mem_map] at hn
  obtain ⟨m, _, rfl⟩ := hn
  simp [drainNode, flushNode]

/-! ## After Drain, all nodes are drained -/

/-- After Drain, every node has drained=true. -/
theorem drain_all_drained (setBlock : Option Bool) (nodes : List NodeState) :
    ∀ n, n ∈ drain setBlock nodes → n.drained = true := by
  intro n hn
  simp [drain, List.mem_map] at hn
  obtain ⟨m, _, rfl⟩ := hn
  simp [drainNode, flushNode]

/-- After Drain, IsDrained returns true. -/
theorem isDrained_after_drain (setBlock : Option Bool) (nodes : List NodeState) :
    isDrained (drain setBlock nodes) = true := by
  rw [isDrained_iff_all_drained]
  exact drain_all_drained setBlock nodes

/-! ## Drain preserves list length (visits every node exactly once) -/

/-- Drain produces a list of the same length as the input,
    modeling that Traverse visits every node exactly once. -/
theorem drain_length (setBlock : Option Bool) (nodes : List NodeState) :
    (drain setBlock nodes).length = nodes.length := by
  simp [drain]

/-! ## Drain with block-input sets blocked on all nodes -/

/-- When Drain is called with setBlockInput=true, all nodes end up blocked. -/
theorem drain_with_block_sets_blocked (nodes : List NodeState) :
    ∀ n, n ∈ drain (some true) nodes → n.blocked = true := by
  intro n hn
  simp [drain, List.mem_map] at hn
  obtain ⟨m, _, rfl⟩ := hn
  simp [drainNode, setBlockInputNode, flushNode]

/-! ## SetBlockInput false unblocks all nodes -/

/-- After SetBlockInput false, every node has blocked=false. -/
theorem setBlockInput_false_all_unblocked (nodes : List NodeState) :
    ∀ n, n ∈ setBlockInput false nodes → n.blocked = false := by
  intro n hn
  simp [setBlockInput, List.mem_map] at hn
  obtain ⟨m, _, rfl⟩ := hn
  simp [setBlockInputNode]

/-! ## SetBlockInput preserves flushed and drained state -/

/-- SetBlockInput does not modify the flushed field. -/
theorem setBlockInput_preserves_flushed (b : Bool) (nodes : List NodeState) :
    ∀ i (h : i < nodes.length),
      ((setBlockInput b nodes)[i]'(by simp [setBlockInput]; omega)).flushed
      = (nodes[i]'h).flushed := by
  intro i hi
  simp [setBlockInput, List.getElem_map]
  rfl

/-- SetBlockInput does not modify the drained field. -/
theorem setBlockInput_preserves_drained (b : Bool) (nodes : List NodeState) :
    ∀ i (h : i < nodes.length),
      ((setBlockInput b nodes)[i]'(by simp [setBlockInput]; omega)).drained
      = (nodes[i]'h).drained := by
  intro i hi
  simp [setBlockInput, List.getElem_map]
  rfl
