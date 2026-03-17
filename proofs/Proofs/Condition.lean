-- Proofs/Condition.lean: Correctness proofs for boolean condition combinators

import Spec.Condition

open Condition

/-! ## AND base cases -/

/-- AND of empty list = true (vacuous truth), matching Go's loop-that-never-iterates. -/
theorem and_empty (v : T) :
    andMatch ([] : List (Condition T)) v = true := by
  rfl

/-- AND of a single condition equals that condition's result. -/
theorem and_singleton (c : Condition T) (v : T) :
    andMatch [c] v = c v := by
  simp [andMatch, List.all_cons, List.all_nil]

/-! ## OR base cases -/

/-- OR of empty list = false, matching Go's loop-that-never-iterates. -/
theorem or_empty (v : T) :
    orMatch ([] : List (Condition T)) v = false := by
  rfl

/-- OR of a single condition equals that condition's result. -/
theorem or_singleton (c : Condition T) (v : T) :
    orMatch [c] v = c v := by
  simp [orMatch, List.any_cons, List.any_nil]

/-! ## NOT -/

/-- NOT(AND([])) = false, since AND([]) = true. -/
theorem not_empty (v : T) :
    notMatch ([] : List (Condition T)) v = false := by
  rfl

/-- NOT inverts AND: notMatch cs v = !(andMatch cs v). -/
theorem not_is_negation_of_and (cs : List (Condition T)) (v : T) :
    notMatch cs v = !(andMatch cs v) := by
  rfl

/-! ## Static -/

/-- Static true always matches. -/
theorem static_true (v : T) :
    (static true : Condition T) v = true := by
  rfl

/-- Static false never matches. -/
theorem static_false (v : T) :
    (static false : Condition T) v = false := by
  rfl

/-! ## In (set membership) -/

/-- In(x, []) = false: empty set contains nothing. -/
theorem in_empty [DecidableEq T] (v : T) :
    inSet ([] : List T) v = false := by
  rfl

/-- In(x, xs ++ [x]) = true: appending x makes it a member. -/
theorem in_append_self [DecidableEq T] (v : T) (xs : List T) :
    inSet (xs ++ [v]) v = true := by
  simp [inSet, List.any_append, List.any_cons, List.any_nil]

/-- In(x, [x]) = true: singleton containing x. -/
theorem in_singleton_self [DecidableEq T] (v : T) :
    inSet [v] v = true := by
  simp [inSet, List.any_cons, List.any_nil]

/-! ## AND / OR cons decomposition (used by later proofs) -/

/-- AND of cons: andMatch (c :: cs) v = (c v && andMatch cs v). -/
theorem and_cons (c : Condition T) (cs : List (Condition T)) (v : T) :
    andMatch (c :: cs) v = (c v && andMatch cs v) := by
  rfl

/-- OR of cons: orMatch (c :: cs) v = (c v || orMatch cs v). -/
theorem or_cons (c : Condition T) (cs : List (Condition T)) (v : T) :
    orMatch (c :: cs) v = (c v || orMatch cs v) := by
  rfl

/-! ## AND with a false element -/

/-- If any condition in the AND list is false on v, the whole AND is false. -/
theorem and_with_false (cs : List (Condition T)) (v : T)
    (h : ∃ c, c ∈ cs ∧ c v = false) :
    andMatch cs v = false := by
  obtain ⟨c, hc_mem, hc_false⟩ := h
  induction cs with
  | nil => exact absurd hc_mem (List.not_mem_nil _)
  | cons d ds ih =>
    rw [and_cons]
    rcases List.mem_cons.mp hc_mem with heq | hmem
    · subst heq; rw [hc_false]; rfl
    · rw [ih hmem]; exact Bool.and_false (d v)

/-- Specialization: AND with a single false-on-v element prepended. -/
theorem and_cons_false (c : Condition T) (cs : List (Condition T)) (v : T)
    (hf : c v = false) :
    andMatch (c :: cs) v = false := by
  rw [and_cons, hf]; rfl

/-! ## OR with a true element -/

/-- If any condition in the OR list is true on v, the whole OR is true. -/
theorem or_with_true (cs : List (Condition T)) (v : T)
    (h : ∃ c, c ∈ cs ∧ c v = true) :
    orMatch cs v = true := by
  obtain ⟨c, hc_mem, hc_true⟩ := h
  induction cs with
  | nil => exact absurd hc_mem (List.not_mem_nil _)
  | cons d ds ih =>
    rw [or_cons]
    rcases List.mem_cons.mp hc_mem with heq | hmem
    · subst heq; rw [hc_true]; rfl
    · rw [ih hmem]; cases d v <;> rfl

/-- Specialization: OR with a single true-on-v element prepended. -/
theorem or_cons_true (c : Condition T) (cs : List (Condition T)) (v : T)
    (ht : c v = true) :
    orMatch (c :: cs) v = true := by
  rw [or_cons, ht]; rfl

/-! ## De Morgan's laws -/

/-- De Morgan: NOT(AND(xs)) iff OR(NOT each x).
    notMatch cs v = orMatch (cs.map (fun c v' => !c v')) v -/
theorem demorgan_not_and (cs : List (Condition T)) (v : T) :
    notMatch cs v = orMatch (cs.map (fun c => (fun v' => !c v' : Condition T))) v := by
  induction cs with
  | nil => rfl
  | cons c cs ih =>
    simp only [notMatch, andMatch, List.all_cons, orMatch, List.map_cons, List.any_cons]
    cases hc : c v
    · rfl
    · simp only [Bool.true_and, Bool.not_true, Bool.false_or]
      exact ih

/-- De Morgan: NOT(OR(xs)) iff AND(NOT each x).
    Uses explicit Bool.not to avoid Prop/Bool ambiguity. -/
theorem demorgan_not_or (cs : List (Condition T)) (v : T) :
    (orMatch cs v).not = andMatch (cs.map (fun c => (fun v' => (c v').not : Condition T))) v := by
  induction cs with
  | nil => rfl
  | cons c cs ih =>
    show (c v || orMatch cs v).not =
         ((c v).not && andMatch (cs.map (fun c => (fun v' => (c v').not : Condition T))) v)
    rw [← ih]
    cases c v <;> rfl

/-! ## AND / OR append decomposition -/

/-- AND distributes over append. -/
theorem and_append (cs1 cs2 : List (Condition T)) (v : T) :
    andMatch (cs1 ++ cs2) v = (andMatch cs1 v && andMatch cs2 v) := by
  induction cs1 with
  | nil => rfl
  | cons c cs1 ih =>
    simp only [List.cons_append, and_cons]
    rw [ih, Bool.and_assoc]

/-- OR distributes over append. -/
theorem or_append (cs1 cs2 : List (Condition T)) (v : T) :
    orMatch (cs1 ++ cs2) v = (orMatch cs1 v || orMatch cs2 v) := by
  induction cs1 with
  | nil => rfl
  | cons c cs1 ih =>
    simp only [List.cons_append, or_cons]
    rw [ih, Bool.or_assoc]

/-! ## Function condition -/

/-- A function condition evaluates to the function applied to v. -/
theorem function_eval (f : T → Bool) (v : T) :
    (Condition.function f) v = f v := by
  rfl

/-! ## CombineConds -/

/-- Combine of empty = none (nil in Go). -/
theorem combine_empty :
    combineConds ([] : List (Condition T)) = CombineResult.none := by
  rfl

/-- Combine of single = single. -/
theorem combine_single (c : Condition T) :
    combineConds [c] = CombineResult.single c := by
  rfl

/-- Combine of two or more = combined (AND). -/
theorem combine_multiple (c1 c2 : Condition T) (cs : List (Condition T)) :
    combineConds (c1 :: c2 :: cs) = CombineResult.combined (c1 :: c2 :: cs) := by
  rfl

/-- Combined result evaluates as AND. -/
theorem combine_eval_is_and (c1 c2 : Condition T) (cs : List (Condition T)) (v : T) :
    (combineConds (c1 :: c2 :: cs)).eval v = some (andMatch (c1 :: c2 :: cs) v) := by
  rfl

/-- Combine flattens nested AND:
    AND of two groups concatenated equals AND of the flattened list.
    This models how CombineConds merges nested And conditions. -/
theorem combine_flatten (cs1 cs2 : List (Condition T)) (v : T) :
    andMatch (cs1 ++ cs2) v = (andMatch cs1 v && andMatch cs2 v) :=
  and_append cs1 cs2 v

/-! ## Double negation -/

/-- Double NOT is equivalent to AND (NOT is defined as negation of AND). -/
theorem not_not_is_and (cs : List (Condition T)) (v : T) :
    !(notMatch cs v) = andMatch cs v := by
  simp [notMatch]

/-! ## AND idempotence / monotonicity -/

/-- AND is monotone: if all cs match, adding more true conditions still matches. -/
theorem and_true_preserves (cs : List (Condition T)) (c : Condition T) (v : T)
    (hcs : andMatch cs v = true) (hc : c v = true) :
    andMatch (c :: cs) v = true := by
  show (c v && andMatch cs v) = true
  simp [hc, hcs]

/-- OR is monotone: if any cs matches, adding more conditions still matches. -/
theorem or_true_preserves (cs : List (Condition T)) (c : Condition T) (v : T)
    (hcs : orMatch cs v = true) :
    orMatch (c :: cs) v = true := by
  show (c v || orMatch cs v) = true
  simp [hcs]

/-! ## In properties -/

/-- If v is in xs, then v is in xs ++ ys. -/
theorem in_append_left [DecidableEq T] (v : T) (xs ys : List T)
    (h : inSet xs v = true) :
    inSet (xs ++ ys) v = true := by
  unfold inSet at *
  rw [List.any_append]
  simp [h]

/-- If v is in ys, then v is in xs ++ ys. -/
theorem in_append_right [DecidableEq T] (v : T) (xs ys : List T)
    (h : inSet ys v = true) :
    inSet (xs ++ ys) v = true := by
  unfold inSet at *
  rw [List.any_append]
  simp [h]

/-- In is equivalent to List.any with equality check. -/
theorem in_iff_elem [DecidableEq T] (v : T) (xs : List T) :
    inSet xs v = xs.any (· == v) := by
  rfl
