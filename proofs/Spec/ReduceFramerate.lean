-- Spec/ReduceFramerate.lean: Formal specification of frame rate reduction from
-- packetorframe/filter/reduceframerate/reduce_framerate_fraction.go

import Spec.Rational

/-- Per-stream state for the reduce-framerate filter. -/
structure ReduceState where
  frameCount : Nat
  deriving Repr

namespace ReduceState

def init : ReduceState := ⟨0⟩

/--
  Mirrors the acceptance logic from reduce_framerate_fraction.go lines 72-85.
  Given fraction num/den, decides whether frame `frameID` should pass.

  Go code:
  ```
  eachN := float64(den) / float64(num)
  nDeviation := math.Remainder(float64(frameID % den), eachN)
  shouldPass := nDeviation >= 0 && nDeviation < 1.0
  ```

  We model this with integer arithmetic to avoid floating-point reasoning.
  The key insight: `math.Remainder(x, y)` returns the IEEE remainder,
  which is `x - round(x/y) * y` where `round` is round-to-nearest-even.

  For our purposes, frame `frameID` passes iff:
    let r = frameID % den
    let q = r * num  (scaled to avoid division)
    the fractional part of (r / eachN) is in [0, 1)
    equivalently: (r * num) % den is in [0, num)

  This is a standard Bresenham-style acceptance criterion.
-/
def shouldPass (num den : Nat) (frameID : Nat) : Bool :=
  if num == 0 then false
  else if den == 0 then false  -- guard
  else
    -- Equivalent integer test: frame passes if (frameID % den) * num % den < num
    -- This avoids floating point entirely.
    let r := frameID % den
    let scaled := r * num
    let remainder := scaled % den
    remainder < num

/-- Step the filter: decide and advance frame counter. -/
def step (s : ReduceState) (num den : Nat) : Bool × ReduceState :=
  let pass := shouldPass num den s.frameCount
  (pass, ⟨s.frameCount + 1⟩)

end ReduceState
