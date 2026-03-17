-- Spec/Units.lean: Formal specification of unit types from types/basic_units.go

/-! # Unit types

  Models the 5 unit types from `types/basic_units.go`:
  - `UB`  (Bytes)
  - `Ub`  (bits)
  - `UBps` (Bytes/s)
  - `Ubps` (bits/s)
  - `US`  (time, modeled as integer ticks)

  All conversions use integer arithmetic.
  The Go code uses float64 for rates, but the core relationships
  (multiply/divide by 8, rate×time=amount) are integer-exact when
  the inputs are integral.
-/

/-- Bytes (mirrors Go `UB int64`). -/
@[ext] structure UB where val : Int deriving Repr, DecidableEq

/-- Bits (mirrors Go `Ub int64`). -/
@[ext] structure Ub where val : Int deriving Repr, DecidableEq

/-- Bytes per second (mirrors Go `UBps float64`, modeled as Int). -/
@[ext] structure UBps where val : Int deriving Repr, DecidableEq

/-- Bits per second (mirrors Go `Ubps float64`, modeled as Int). -/
@[ext] structure Ubps where val : Int deriving Repr, DecidableEq

/-- Time duration in seconds (mirrors Go `US time.Duration`, modeled as Int seconds). -/
@[ext] structure US where val : Int deriving Repr, DecidableEq

namespace Units

/-! ## Byte ↔ Bit conversions -/

/-- `UB.Tob()`: Bytes → bits by multiplying by 8. -/
def tob (v : UB) : Ub := ⟨v.val * 8⟩

/-- `Ub.ToB()`: bits → Bytes by integer-dividing by 8. -/
def toB (v : Ub) : UB := ⟨v.val / 8⟩

/-! ## Rate conversions (Bytes/s ↔ bits/s) -/

/-- `UBps.Tobps()`: Bytes/s → bits/s by multiplying by 8. -/
def tobps (v : UBps) : Ubps := ⟨v.val * 8⟩

/-- `Ubps.ToBps()`: bits/s → Bytes/s by integer-dividing by 8. -/
def toBps (v : Ubps) : UBps := ⟨v.val / 8⟩

/-! ## Rate × Time = Amount -/

/-- `UBps.ToB(t)`: Bytes/s × seconds → Bytes. -/
def rateTimeToBytes (r : UBps) (t : US) : UB := ⟨r.val * t.val⟩

/-- `Ubps.Tob(t)`: bits/s × seconds → bits. -/
def rateTimeToBits (r : Ubps) (t : US) : Ub := ⟨r.val * t.val⟩

/-! ## Amount / Rate = Time -/

/-- `UB.ToS(r)`: Bytes / (Bytes/s) → seconds. -/
def bytesToTime (v : UB) (r : UBps) : US := ⟨v.val / r.val⟩

/-- `Ub.ToS(r)`: bits / (bits/s) → seconds. -/
def bitsToTime (v : Ub) (r : Ubps) : US := ⟨v.val / r.val⟩

/-! ## Amount → Rate (given time) -/

/-- `UB.ToBps(t)`: Bytes / seconds → Bytes/s. -/
def bytesToRate (v : UB) (t : US) : UBps := ⟨v.val / t.val⟩

/-- `Ub.Tobps(t)`: bits / seconds → bits/s. -/
def bitsToRate (v : Ub) (t : US) : Ubps := ⟨v.val / t.val⟩

end Units
