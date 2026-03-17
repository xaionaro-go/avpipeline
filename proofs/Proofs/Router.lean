-- Proofs/Router.lean: Correctness proofs for router publisher conflict resolution

import Spec.Router

open RouteState PublishMode AddResult

/-! ## Basic safety: closed route rejects publishers -/

theorem closed_route_rejects (s : RouteState) (p : Publisher) (h : s.isOpen = false) :
    (s.addPublisher p).1 = errRouteClosed := by
  simp [addPublisher, h]

/-! ## Duplicate publisher detection -/

theorem duplicate_publisher_rejected (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hDup : s.publishers.any (· == p) = true) :
    (s.addPublisher p).1 = errAlreadyAPublisher := by
  simp [addPublisher, hOpen, hDup]

/-! ## ExclusiveFail always fails when publishers exist -/

theorem exclusive_fail_with_publishers (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = exclusiveFail) :
    (s.addPublisher p).1 = errAlreadyHasPublisher := by
  simp [addPublisher, hOpen, hNotDup, hNonEmpty, hMode]

/-! ## ExclusiveTakeover clears all existing publishers -/

theorem exclusive_takeover_clears (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = exclusiveTakeover) :
    (s.addPublisher p).1 = ok [p] := by
  simp [addPublisher, hOpen, hNotDup, hNonEmpty, hMode]

theorem exclusive_takeover_result_is_singleton (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = exclusiveTakeover) :
    (s.addPublisher p).2.publishers = [p] := by
  simp [addPublisher, hOpen, hNotDup, hNonEmpty, hMode]

/-! ## SharedFail with exclusive publisher present fails -/

theorem shared_fail_with_exclusive (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = sharedFail)
    (hHasExcl : s.publishers.any (fun pub => pub.mode.isExclusive) = true) :
    (s.addPublisher p).1 = errAlreadyHasPublisher := by
  simp only [addPublisher, hOpen, hNotDup, hNonEmpty, hMode, Bool.false_eq_true,
    ↓reduceIte, Bool.not_true]
  rw [List.any_eq_true] at hHasExcl
  obtain ⟨x, hx_mem, hx_excl⟩ := hHasExcl
  have : (s.publishers.any fun pub => pub.mode.isExclusive) = true := by
    rw [List.any_eq_true]
    exact ⟨x, hx_mem, hx_excl⟩
  simp [this]

/-! ## SharedFail with no exclusive publishers succeeds -/

theorem shared_fail_no_exclusive_succeeds (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = sharedFail)
    (hNoExcl : s.publishers.any (fun pub => pub.mode.isExclusive) = false) :
    ∃ pubs, (s.addPublisher p).1 = ok pubs := by
  simp only [addPublisher, hOpen, hNotDup, hNonEmpty, hMode, ↓reduceIte]
  rw [List.any_eq_false] at hNoExcl
  have hNotEx : ¬ (∃ x, x ∈ s.publishers ∧ x.mode.isExclusive = true) := by
    intro ⟨x, hx_mem, hx_excl⟩
    exact absurd hx_excl (hNoExcl x hx_mem)
  simp [hNotEx]

/-! ## SharedTakeover keeps only non-exclusive publishers -/

theorem shared_takeover_removes_exclusive (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hNotDup : s.publishers.any (· == p) = false)
    (hNonEmpty : s.publishers.isEmpty = false)
    (hMode : p.mode = sharedTakeover) :
    (s.addPublisher p).2.publishers =
      (s.publishers.filter (fun pub => !pub.mode.isExclusive)) ++ [p] := by
  simp [addPublisher, hOpen, hNotDup, hNonEmpty, hMode]

/-! ## Adding to empty route always succeeds (if open) -/

theorem empty_route_add_succeeds (s : RouteState) (p : Publisher)
    (hOpen : s.isOpen = true)
    (hEmpty : s.publishers = []) :
    (s.addPublisher p).1 = ok [p] := by
  simp [addPublisher, hOpen, hEmpty, List.isEmpty, List.any]

/-! ## State invariant: isOpen is preserved by addPublisher -/

theorem add_preserves_open (s : RouteState) (p : Publisher) :
    (s.addPublisher p).2.isOpen = s.isOpen := by
  unfold addPublisher
  simp only
  split
  · rfl  -- closed route
  · split
    · rfl  -- duplicate
    · split
      · rfl  -- empty list: ok
      · -- non-empty: match on mode
        split
        · rfl  -- exclusiveTakeover
        · rfl  -- exclusiveFail
        · rfl  -- sharedTakeover
        · split  -- sharedFail
          · rfl
          · rfl
        · rfl  -- undefined
