-- Spec/Condition.lean: Formal specification of boolean condition combinators
-- from types/condition/ and node/condition/

/-!
  Models the generic boolean condition combinators used in avpipeline:
  AND, OR, NOT, In, Static, Function, and CombineConds.

  In Go these are parameterized by `[T any]` with `context.Context`.
  We abstract away the context/type parameter: a condition is simply `T → Bool`,
  and combinators operate on lists of such conditions.
-/

/-- A condition is a function from a value to Bool, abstracting Go's
    `types.Condition[T]` interface with its `Match(ctx, v)` method. -/
def Condition (T : Type) := T → Bool

namespace Condition

variable {T : Type}

/-- AND: match iff all conditions match. Empty AND = true.
    Mirrors `And[T].Match()` in types/condition/and.go. -/
def andMatch (conds : List (Condition T)) (v : T) : Bool :=
  conds.all (· v)

/-- OR: match iff any condition matches. Empty OR = false.
    Mirrors `Or[T].Match()` in types/condition/or.go. -/
def orMatch (conds : List (Condition T)) (v : T) : Bool :=
  conds.any (· v)

/-- NOT: negation of AND of conditions.
    Mirrors `Not[T].Match()` = `!And[T](n).Match(ctx, v)`. -/
def notMatch (conds : List (Condition T)) (v : T) : Bool :=
  !(andMatch conds v)

/-- Static: always returns the given boolean value.
    Mirrors `Static[T].Match()` in types/condition/static.go. -/
def static (b : Bool) : Condition T :=
  fun _ => b

/-- Function: wraps an arbitrary function as a condition.
    Mirrors `Function[T].Match()` in types/condition/function.go. -/
def function (f : T → Bool) : Condition T := f

/-- In: checks if a value is in a given set (by decidable equality).
    Mirrors `In.Match()` in node/condition/in.go. -/
def inSet [DecidableEq T] (set : List T) (v : T) : Bool :=
  set.any (· == v)

/-- CombineConds result type: Go returns nil for empty, single for one,
    And for multiple. We model this as Option (to capture the nil case). -/
inductive CombineResult (T : Type) where
  | none                           -- 0 conditions → nil
  | single (c : Condition T)       -- 1 condition → that condition
  | combined (cs : List (Condition T))  -- 2+ conditions → And of all
  deriving Inhabited

/-- CombineConds: combines conditions, mirroring combine.go logic. -/
def combineConds (conds : List (Condition T)) : CombineResult T :=
  match conds with
  | []  => .none
  | [c] => .single c
  | cs  => .combined cs

/-- Evaluate a CombineResult as an AND match (the combined semantics). -/
def CombineResult.eval (cr : CombineResult T) (v : T) : Option Bool :=
  match cr with
  | .none        => Option.none
  | .single c    => some (c v)
  | .combined cs => some (andMatch cs v)

end Condition
