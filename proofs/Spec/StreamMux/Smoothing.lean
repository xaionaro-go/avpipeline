-- Spec/StreamMux/Smoothing.lean: Formal specification of inertial smoothing
-- and FPS fraction packing from preset/streammux/stream_mux.go

/-!
  ## Inertial Smoothing

  Models `updateWithInertialValue` (line 1301) using integer rational arithmetic.

  Go code:
    effectiveInertia = inertia * (count / (count + 3))
    result = old * effectiveInertia + new * (1 - effectiveInertia)

  We represent inertia as a rational `inertiaNum/inertiaDen` and compute
  everything in exact integer arithmetic with a common denominator, avoiding
  floating-point entirely.

  The effective inertia is inertiaNum * count / (inertiaDen * (count + 3)).
  The complement is (inertiaDen * (count + 3) - inertiaNum * count) / (inertiaDen * (count + 3)).

  The smoothed result numerator (over the common denominator) is:
    old * inertiaNum * count + new * (inertiaDen * (count + 3) - inertiaNum * count)

  ## FPS Fraction Packing

  Models `SetFPSFraction` (line 1187) and `GetFPSFraction` (line 1204).

  Go code:
    pack:   (num << 32) | den
    unpack: num = value >> 32, den = value & 0xFFFFFFFF
-/

namespace StreamMux

/-! ### Inertial Smoothing (rational arithmetic) -/

/-- Effective inertia numerator: `inertiaNum * count`.
    The denominator is `inertiaDen * (count + 3)`.
    This models `inertia * (count / (count + 3))`. -/
def effectiveInertiaNum (inertiaNum : Nat) (count : Nat) : Nat :=
  inertiaNum * count

/-- Effective inertia denominator: `inertiaDen * (count + 3)`. -/
def effectiveInertiaDen (inertiaDen : Nat) (count : Nat) : Nat :=
  inertiaDen * (count + 3)

/-- The weight given to the old value in the smoothed result.
    This is the effective inertia numerator: inertiaNum * count. -/
def weightOld (inertiaNum : Nat) (count : Nat) : Nat :=
  inertiaNum * count

/-- The weight given to the new value in the smoothed result.
    This is den - num of the effective inertia:
    inertiaDen * (count + 3) - inertiaNum * count.
    Requires inertiaNum * count ≤ inertiaDen * (count + 3). -/
def weightNew (inertiaNum inertiaDen : Nat) (count : Nat) : Nat :=
  inertiaDen * (count + 3) - inertiaNum * count

/-- The common denominator: inertiaDen * (count + 3).
    weightOld + weightNew = smoothedDen when the inertia is valid. -/
def smoothedDen (inertiaDen : Nat) (count : Nat) : Nat :=
  inertiaDen * (count + 3)

/-- The smoothed result numerator over the common denominator.
    result = old * weightOld + new * weightNew -/
def smoothedNum (old new_ : Nat) (inertiaNum inertiaDen : Nat) (count : Nat) : Nat :=
  old * weightOld inertiaNum count + new_ * weightNew inertiaNum inertiaDen count

/-! ### FPS Fraction Packing (bitwise) -/

/-- Pack numerator and denominator into a single Nat.
    Mirrors Go: `(uint64(num) << 32) | uint64(den)`. -/
def packFPS (num den : Nat) : Nat :=
  num * 2^32 + den

/-- Unpack numerator from a packed value.
    Mirrors Go: `numDen >> 32`. -/
def unpackFPSNum (packed : Nat) : Nat :=
  packed / 2^32

/-- Unpack denominator from a packed value.
    Mirrors Go: `numDen & 0xFFFFFFFF`. -/
def unpackFPSDen (packed : Nat) : Nat :=
  packed % 2^32

end StreamMux
