-- Proofs/Rational.lean: Correctness proofs for Rational arithmetic

import Spec.Rational

open Rational

/-! ## Reverse properties -/

/-- Reverse is an involution: reverse(reverse(r)) = r -/
theorem reverse_involution (r : Rational) :
    r.reverse.reverse = r := by
  simp [reverse]

/-- Reverse swaps num and den -/
theorem reverse_swap (r : Rational) :
    r.reverse.num = r.den ∧ r.reverse.den = r.num := by
  simp [reverse]

/-! ## Multiplication properties -/

/-- Multiplication is commutative -/
theorem mul_comm (a b : Rational) :
    a.mul b = b.mul a := by
  simp [mul, Rational.mk.injEq]
  exact ⟨Int.mul_comm a.num b.num, Int.mul_comm a.den b.den⟩

/-- Multiplication is associative -/
theorem mul_assoc (a b c : Rational) :
    (a.mul b).mul c = a.mul (b.mul c) := by
  simp [mul, Rational.mk.injEq]
  exact ⟨Int.mul_assoc a.num b.num c.num, Int.mul_assoc a.den b.den c.den⟩

/-- Multiplication by identity (1/1) is neutral -/
theorem mul_one_right (r : Rational) :
    r.mul ⟨1, 1⟩ = r := by
  simp [mul]

theorem mul_one_left (r : Rational) :
    (Rational.mk' 1 1).mul r = r := by
  simp [mul, mk']

/-! ## Division properties -/

/-- div a b = mul a (reverse b) — division is multiplication by reciprocal -/
theorem div_eq_mul_reverse (a b : Rational) :
    a.div b = a.mul b.reverse := by
  simp [div, mul, reverse]

/-! ## Reverse interacts with mul/div -/

/-- reverse(a.mul b) = (reverse a).mul (reverse b) -/
theorem reverse_mul (a b : Rational) :
    (a.mul b).reverse = (a.reverse).mul (b.reverse) := by
  simp [mul, reverse]

/-- reverse distributes over div: reverse(a.div b) = (reverse a).div (reverse b) -/
theorem reverse_div (a b : Rational) :
    (a.div b).reverse = (a.reverse).div (b.reverse) := by
  simp [div, reverse]

/-! ## The key pipeline computation: timeBase.Reverse().Div(maxFPS)

  This is used in limit_framerate.go line 76:
    minDuration := timeBase.Reverse().Div(maxFPS)

  We prove this equals the expected structure: ⟨tb.den * fps.den, tb.num * fps.num⟩
-/

theorem minDuration_structure (timeBase maxFPS : Rational) :
    (timeBase.reverse.div maxFPS) =
      ⟨timeBase.den * maxFPS.den, timeBase.num * maxFPS.num⟩ := by
  simp [reverse, div]

/-! ## Zero denominator detection -/

/-- If den = 0, reverse produces num = 0 -/
theorem reverse_zero_den (r : Rational) (h : r.den = 0) :
    r.reverse.num = 0 := by
  simp [reverse, h]

/-- Multiplying anything by a zero-num rational produces zero num -/
theorem mul_zero_num (a b : Rational) (h : b.num = 0) :
    (a.mul b).num = 0 := by
  simp [mul, h]
