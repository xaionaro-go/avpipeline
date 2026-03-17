-- Spec/Rational.lean: Formal specification of Rational type from types/rational.go

/-- Rational number as Num/Den pair, mirroring Go's `types.Rational{Num, Den int}`. -/
structure Rational where
  num : Int
  den : Int
  deriving Repr, DecidableEq

namespace Rational

def mk' (n d : Int) : Rational := ⟨n, d⟩

/-- Mirrors Go: `Rational{Num: r.Den, Den: r.Num}` -/
def reverse (r : Rational) : Rational :=
  ⟨r.den, r.num⟩

/-- Mirrors Go: `Rational{Num: r.Num * other.Num, Den: r.Den * other.Den}` -/
def mul (r other : Rational) : Rational :=
  ⟨r.num * other.num, r.den * other.den⟩

/-- Mirrors Go: `Rational{Num: r.Num * other.Den, Den: r.Den * other.Num}` -/
def div (r other : Rational) : Rational :=
  ⟨r.num * other.den, r.den * other.num⟩

/-- The rational value as a real-number fraction (for specification purposes). -/
noncomputable def toReal (r : Rational) : Float :=
  r.num.toNat.toFloat / r.den.toNat.toFloat

instance : Mul Rational := ⟨mul⟩
instance : ToString Rational := ⟨fun r => s!"{r.num}/{r.den}"⟩

end Rational
