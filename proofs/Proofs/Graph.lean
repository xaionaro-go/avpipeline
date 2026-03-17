-- Proofs/Graph.lean: Correctness proofs for graph traversal logic

import Spec.Graph

open Graph

/-! ## Helper: Nodup for append with singleton -/

private theorem nodup_append_singleton (l : List Nat) (x : Nat)
    (hl : l.Nodup) (hx : x ∉ l) : (l ++ [x]).Nodup := by
  simp [List.Nodup]
  rw [List.pairwise_append]
  exact ⟨hl, List.pairwise_singleton _ x,
    fun a ha b hb => by simp at hb; subst hb; exact fun heq => hx (heq ▸ ha)⟩

/-! ## DFS unfolding lemmas -/

theorem dfs_unfold_cons (g : Graph) (fuel : Nat) (visited : List Nat) (n : Nat) (rest : List Nat) :
    dfs g (fuel + 1) visited (n :: rest) =
      if visited.contains n then
        dfs g fuel visited rest
      else
        dfs g fuel (dfs g fuel (visited ++ [n]) (g.nextLayer n)) rest := by
  simp [dfs]

theorem dfs_unfold_nil (g : Graph) (fuel : Nat) (visited : List Nat) :
    dfs g (fuel + 1) visited [] = visited := by
  simp [dfs]

/-! ## nextLayer deduplication -/

/-- Helper: foldl dedup produces no duplicates. -/
theorem foldl_dedup_nodup (input : List Nat) :
    (input.foldl (fun (acc : List Nat) x =>
      if acc.contains x then acc else acc ++ [x]) []).Nodup := by
  suffices h : ∀ (acc : List Nat), acc.Nodup →
    (input.foldl (fun (acc : List Nat) x =>
      if acc.contains x then acc else acc ++ [x]) acc).Nodup by
    exact h [] List.nodup_nil
  intro acc hacc
  induction input generalizing acc with
  | nil => exact hacc
  | cons x xs ih =>
    simp only [List.foldl_cons]
    split
    · exact ih acc hacc
    · rename_i hne
      apply ih
      apply nodup_append_singleton
      · exact hacc
      · intro hmem
        exact hne (List.contains_iff_mem.mpr hmem)

/-- `nextLayer` output has no duplicates.
    Mirrors the dedup logic in next_layer.go using the `isSet` map. -/
theorem nextLayer_nodup (g : Graph) (n : Nat) :
    (g.nextLayer n).Nodup :=
  foldl_dedup_nodup (g.neighbors n)

/-! ## nextLayer subset -/

/-- Helper: foldl dedup output ⊆ input ∪ accumulator. -/
private theorem foldl_dedup_mem (input : List Nat) (acc : List Nat) (x : Nat)
    (h : x ∈ input.foldl (fun (a : List Nat) y =>
      if a.contains y then a else a ++ [y]) acc) :
    x ∈ acc ∨ x ∈ input := by
  induction input generalizing acc with
  | nil => exact Or.inl h
  | cons y ys ih =>
    simp only [List.foldl_cons] at h
    have := ih _ h
    cases this with
    | inl hmem =>
      split at hmem
      · exact (Or.inl hmem).imp_right (List.mem_cons_of_mem y)
      · rcases List.mem_append.mp hmem with h | h
        · exact (Or.inl h).imp_right (List.mem_cons_of_mem y)
        · simp at h; exact Or.inr (h ▸ List.mem_cons_self y ys)
    | inr hmem => exact Or.inr (List.mem_cons_of_mem y hmem)

/-- `nextLayer` output ⊆ adjacency list neighbors.
    Mirrors: nextLayer only returns nodes from GetPushTos. -/
theorem nextLayer_subset (g : Graph) (n : Nat) (x : Nat)
    (h : x ∈ g.nextLayer n) :
    x ∈ g.neighbors n := by
  unfold nextLayer at h
  have := foldl_dedup_mem (g.neighbors n) [] x h
  cases this with
  | inl hmem => exact absurd hmem (List.not_mem_nil x)
  | inr hmem => exact hmem

/-! ## DFS visited set properties -/

/-- Visited set grows monotonically: dfs only adds to visited, never removes.
    Mirrors: alreadyVisited map only gets `alreadyVisited[n] = struct{}{}` assignments. -/
theorem dfs_visited_monotone (g : Graph) (fuel : Nat) (visited roots : List Nat) :
    ∀ x, x ∈ visited → x ∈ dfs g fuel visited roots := by
  induction fuel generalizing visited roots with
  | zero => intros; simp [dfs]; assumption
  | succ fuel' ih =>
    intro x hx
    match roots with
    | [] => simp [dfs]; exact hx
    | n :: rest =>
      simp only [dfs]
      split
      · exact ih visited rest x hx
      · apply ih
        apply ih
        exact List.mem_append_left [n] hx

/-- Each node visited at most once: no duplicates in visited set after DFS.
    Mirrors: `if isVisited { continue }` in traverse.go line 47-49. -/
theorem dfs_nodup (g : Graph) (fuel : Nat) (visited roots : List Nat)
    (hv : visited.Nodup) :
    (dfs g fuel visited roots).Nodup := by
  induction fuel generalizing visited roots with
  | zero => simp [dfs]; exact hv
  | succ fuel' ih =>
    match roots with
    | [] => simp [dfs]; exact hv
    | n :: rest =>
      rw [dfs_unfold_cons]
      split
      · exact ih visited rest hv
      · rename_i hne
        apply ih
        apply ih
        apply nodup_append_singleton _ _ hv
        intro hmem
        exact hne (List.contains_iff_mem.mpr hmem)

/-- Cycle detection: a node already in visited is skipped (not re-processed).
    Mirrors: `_, isVisited := alreadyVisited[n]; if isVisited { continue }` -/
theorem dfs_skip_visited (g : Graph) (fuel : Nat) (visited rest : List Nat) (n : Nat)
    (hv : visited.contains n = true) :
    dfs g (fuel + 1) visited (n :: rest) = dfs g fuel visited rest := by
  rw [dfs_unfold_cons, if_pos hv]

/-- Empty graph traversal produces empty visited set.
    Mirrors: traversing with no roots yields no visits. -/
theorem traverse_empty_roots (g : Graph) (fuel : Nat) :
    traverse g fuel [] = [] := by
  unfold traverse
  cases fuel with
  | zero => simp [dfs]
  | succ n => simp [dfs]

/-- Traversal with no duplicates in result (consequence of dfs_nodup). -/
theorem traverse_nodup (g : Graph) (fuel : Nat) (roots : List Nat) :
    (traverse g fuel roots).Nodup :=
  dfs_nodup g fuel [] roots List.nodup_nil

/-! ## DFS contains monotonicity (Bool version) -/

theorem dfs_visited_contains_monotone (g : Graph) (fuel : Nat) (visited roots : List Nat) (x : Nat)
    (h : visited.contains x = true) :
    (dfs g fuel visited roots).contains x = true :=
  List.contains_iff_mem.mpr (dfs_visited_monotone g fuel visited roots x (List.contains_iff_mem.mp h))

/-! ## Root is visited -/

/-- Roots are visited when there is enough fuel (single root). -/
theorem dfs_visits_root (g : Graph) (fuel : Nat) (visited : List Nat) (n : Nat)
    (hfuel : fuel ≥ 1) :
    n ∈ dfs g fuel visited [n] := by
  match fuel, hfuel with
  | fuel' + 1, _ =>
    simp only [dfs]
    split
    · rename_i hc
      exact dfs_visited_monotone g fuel' visited [] n (List.contains_iff_mem.mp hc)
    · apply dfs_visited_monotone
      apply dfs_visited_monotone
      exact List.mem_append_right visited (List.mem_cons_self n [])

/-! ## Empty graph traversal only visits roots -/

private theorem neighbors_empty (n : Nat) : neighbors [] n = [] := by
  simp [neighbors]

private theorem nextLayer_empty (n : Nat) : nextLayer [] n = [] := by
  simp [nextLayer, neighbors_empty, List.foldl]

/-- Helper: dfs on empty graph only produces elements from roots and visited. -/
private theorem dfs_empty_graph_subset (fuel : Nat) (visited roots : List Nat) (x : Nat)
    (h : x ∈ dfs [] fuel visited roots) :
    x ∈ visited ∨ x ∈ roots := by
  induction fuel generalizing visited roots with
  | zero => simp [dfs] at h; exact Or.inl h
  | succ fuel' ih =>
    match roots with
    | [] => simp [dfs] at h; exact Or.inl h
    | n :: rest =>
      rw [dfs_unfold_cons] at h
      split at h
      · exact (ih visited rest h).imp_right (List.mem_cons_of_mem n)
      · rw [nextLayer_empty] at h
        have inner_eq : dfs [] fuel' (visited ++ [n]) [] = visited ++ [n] := by
          cases fuel' with
          | zero => simp [dfs]
          | succ k => simp [dfs]
        rw [inner_eq] at h
        exact (ih (visited ++ [n]) rest h).elim
          (fun hmem => (List.mem_append.mp hmem).elim
            Or.inl
            (fun h2 => by simp at h2; exact Or.inr (h2 ▸ List.mem_cons_self n rest)))
          (fun hmem => Or.inr (List.mem_cons_of_mem n hmem))

/-- Empty graph (no edges) traversal only visits the roots themselves. -/
theorem traverse_empty_graph (fuel : Nat) (roots : List Nat) :
    ∀ x, (traverse [] fuel roots).contains x = true → x ∈ roots := by
  intro x hx
  have hmem := List.contains_iff_mem.mp hx
  exact (dfs_empty_graph_subset fuel [] roots x hmem).elim
    (fun h => absurd h (List.not_mem_nil x))
    id

/-! ## find correctness -/

/-- If find returns true, the target is in the traversal result. -/
theorem find_iff_in_traversal (g : Graph) (fuel : Nat) (roots : List Nat) (target : Nat) :
    find g fuel roots target = true ↔ (traverse g fuel roots).contains target = true := by
  simp [find]

/-- find on empty roots always returns false. -/
theorem find_empty_roots (g : Graph) (fuel : Nat) (target : Nat) :
    find g fuel [] target = false := by
  unfold find traverse
  cases fuel with
  | zero => simp [dfs]
  | succ n => simp [dfs]

/-- find returns true implies target is reachable. -/
theorem find_true_implies_reachable (g : Graph) (fuel : Nat) (roots : List Nat) (target : Nat)
    (h : find g fuel roots target = true) :
    reachable g roots target :=
  ⟨fuel, (find_iff_in_traversal g fuel roots target).mp h⟩
