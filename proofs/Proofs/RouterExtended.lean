-- Proofs/RouterExtended.lean: Correctness proofs for extended router operations

import Spec.RouterExtended

open RouteState Publisher PublishMode AddResult RemoveResult LifecycleResult GetRouteMode GetRouteResult

/-! ## Helper lemma: filter-out of a unique element decreases length by 1 -/

private theorem filter_ne_length {α : Type} [DecidableEq α] (l : List α) (a : α)
    (hMem : a ∈ l) (hNoDup : l.Nodup) :
    (l.filter (fun x => x != a)).length + 1 = l.length := by
  induction l with
  | nil => exact absurd hMem (List.not_mem_nil _)
  | cons h t ih =>
    obtain ⟨hNotIn, hNoDupT⟩ := List.nodup_cons.mp hNoDup
    simp only [List.filter]
    by_cases heq : h = a
    · subst heq
      simp only [bne_self_eq_false, Bool.false_eq_true, ↓reduceIte, List.length_cons]
      have : List.filter (fun x => x != h) t = t := by
        rw [List.filter_eq_self]
        intro x hx
        simp only [bne_iff_ne, ne_eq, decide_eq_true_eq]
        intro hxp; subst hxp; exact absurd hx hNotIn
      rw [this]
    · have hneq : (h != a) = true := by simp [bne_iff_ne, heq]
      simp only [hneq, ↓reduceIte, List.length_cons]
      have hMemT : a ∈ t := by
        cases hMem with
        | head => exact absurd rfl heq
        | tail _ hmt => exact hmt
      have := ih hMemT hNoDupT
      omega

/-! ## RemovePublisher: existing publisher decreases list length by 1 -/

theorem remove_existing_decreases_length (s : RouteState) (p : Publisher)
    (hMem : p ∈ s.publishers)
    (hNoDup : s.publishers.Nodup) :
    ∃ pubs, (s.removePublisher p).1 = RemoveResult.ok pubs ∧
      pubs.length + 1 = s.publishers.length := by
  unfold removePublisher
  have hAny : s.publishers.any (· == p) = true := by
    rw [List.any_eq_true]
    exact ⟨p, hMem, by simp [BEq.beq]⟩
  simp only [hAny, ↓reduceIte]
  exact ⟨_, rfl, filter_ne_length s.publishers p hMem hNoDup⟩

/-! ## RemovePublisher: non-existing publisher returns error -/

theorem remove_nonexisting_errors (s : RouteState) (p : Publisher)
    (hNotMem : p ∉ s.publishers) :
    (s.removePublisher p).1 = RemoveResult.errPublisherNotFound := by
  unfold removePublisher
  have hAny : s.publishers.any (· == p) = false := by
    rw [List.any_eq_false]
    intro x hx
    simp only [beq_iff_eq]
    intro heq; subst heq; exact absurd hx hNotMem
  simp [hAny]

/-! ## RemovePublisher: other publishers unchanged -/

theorem remove_preserves_others (s : RouteState) (p q : Publisher)
    (hNeq : p ≠ q)
    (hMem : q ∈ s.publishers)
    (hAny : s.publishers.any (· == p) = true) :
    q ∈ (s.removePublisher p).2.publishers := by
  unfold removePublisher
  simp only [hAny, ↓reduceIte]
  simp only [List.mem_filter, bne_iff_ne, ne_eq, decide_eq_true_eq]
  exact ⟨hMem, Ne.symm hNeq⟩

/-! ## Close then AddPublisher returns errRouteClosed -/

theorem close_then_add_errors (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true) :
    let s' := (s.closeNode).2
    (s'.addPublisher p).1 = AddResult.errRouteClosed := by
  simp [closeNode, hOpen, addPublisher]

/-! ## Open → Close → Open is valid cycle -/

theorem open_close_open_cycle (s : RouteState)
    (hClosed : s.isOpen = false) :
    let s1 := (s.openNode).2
    let s2 := (s1.closeNode).2
    let result3 := (s2.openNode)
    result3.1 = LifecycleResult.ok ∧ result3.2.isOpen = true := by
  simp [openNode, closeNode, hClosed]

/-! ## Close is idempotent (double close returns ErrAlreadyClosed) -/

theorem close_idempotent (s : RouteState)
    (hOpen : s.isOpen = true) :
    let s' := (s.closeNode).2
    (s'.closeNode).1 = LifecycleResult.errAlreadyClosed := by
  simp [closeNode, hOpen]

/-! ## Open is idempotent (double open returns ErrAlreadyOpen) -/

theorem open_idempotent (s : RouteState)
    (hClosed : s.isOpen = false) :
    let s' := (s.openNode).2
    (s'.openNode).1 = LifecycleResult.errAlreadyOpen := by
  simp [openNode, hClosed]

/-! ## GetRoute modes -/

theorem get_route_fail_existing (routes : RouteMap) (path : String)
    (hExists : routes.any (· == path) = true) :
    (getRoute routes path failIfNotFound).1 = found path := by
  simp [getRoute, hExists]

theorem get_route_fail_missing (routes : RouteMap) (path : String)
    (hNotExists : routes.any (· == path) = false) :
    (getRoute routes path failIfNotFound).1 = errNotFound := by
  simp [getRoute, hNotExists]

theorem get_route_create_if_needed_existing (routes : RouteMap) (path : String)
    (hExists : routes.any (· == path) = true) :
    (getRoute routes path createIfNotFound).1 = found path := by
  simp [getRoute, hExists]

theorem get_route_create_if_needed_missing (routes : RouteMap) (path : String)
    (hNotExists : routes.any (· == path) = false) :
    (getRoute routes path createIfNotFound).1 = created path := by
  simp [getRoute, hNotExists]

theorem get_route_create_always_existing (routes : RouteMap) (path : String)
    (hExists : routes.any (· == path) = true) :
    (getRoute routes path createAlways).1 = errAlreadyExists := by
  simp [getRoute, hExists]

theorem get_route_create_always_missing (routes : RouteMap) (path : String)
    (hNotExists : routes.any (· == path) = false) :
    (getRoute routes path createAlways).1 = created path := by
  simp [getRoute, hNotExists]

/-! ## Route map: add then get = found -/

theorem add_then_get_found (routes : RouteMap) (path : String) :
    (addRoute routes path).any (· == path) = true := by
  simp [addRoute, List.any_cons, BEq.beq]

/-! ## Route map: remove then get = not found -/

theorem remove_then_get_not_found (routes : RouteMap) (path : String) :
    (removeRoute routes path).any (· == path) = false := by
  simp only [removeRoute]
  rw [List.any_eq_false]
  intro x hx
  simp only [List.mem_filter, bne_iff_ne, ne_eq, decide_eq_true_eq] at hx
  obtain ⟨_, hneq⟩ := hx
  simp only [beq_iff_eq]
  intro heq; subst heq; exact absurd rfl hneq

/-! ## Lifecycle preserves publishers -/

theorem open_preserves_publishers (s : RouteState) :
    (s.openNode).2.publishers = s.publishers := by
  unfold openNode; split <;> simp

theorem close_preserves_publishers (s : RouteState) :
    (s.closeNode).2.publishers = s.publishers := by
  unfold closeNode; split <;> simp

/-! ## RemovePublisher preserves isOpen -/

theorem remove_preserves_open (s : RouteState) (p : Publisher) :
    (s.removePublisher p).2.isOpen = s.isOpen := by
  unfold removePublisher; split <;> simp
