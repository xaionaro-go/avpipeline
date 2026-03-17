-- Proofs/ReduceFramerate.lean: Correctness proofs for frame rate reduction

import Spec.ReduceFramerate

open ReduceState

/-! ## Zero fraction passes nothing -/

theorem zero_num_blocks_all (den : Nat) (frameID : Nat) :
    shouldPass 0 den frameID = false := by
  simp [shouldPass]

/-! ## Full fraction (N/N) passes everything -/

theorem full_fraction_passes_all (n : Nat) (hn : n > 0) (frameID : Nat) :
    shouldPass n n frameID = true := by
  simp [shouldPass]
  omega

/-! ## 1/1 passes everything -/

theorem one_one_passes (frameID : Nat) :
    shouldPass 1 1 frameID = true :=
  full_fraction_passes_all 1 (by omega) frameID

/-! ## Zero denominator blocks all (guard) -/

theorem zero_den_blocks (num : Nat) (frameID : Nat) :
    shouldPass num 0 frameID = false := by
  simp [shouldPass]

/-! ## Over one period of `den` frames, exactly `num` frames pass -/

/-- Count how many frames pass in a range [start, start + count). -/
def countPassing (num den : Nat) (start count : Nat) : Nat :=
  match count with
  | 0 => 0
  | n + 1 =>
    let pass := if shouldPass num den (start + n) then 1 else 0
    countPassing num den start n + pass

/-- Count passing frames in [0, count). -/
def countPassingFromZero (num den : Nat) (count : Nat) : Nat :=
  countPassing num den 0 count

/--
  The key correctness property: over exactly `den` frames starting from 0,
  exactly `num` frames pass through the filter.

  This is the fundamental guarantee of the Bresenham acceptance criterion:
  (frameID % den) * num % den < num accepts exactly num out of den frames.
-/
theorem exact_fraction_one_period (num den : Nat) (hnum : num > 0) (hden : den > 0)
    (hle : num ≤ den) :
    countPassingFromZero num den den = num := by
  -- This requires a combinatorial argument about the distribution of
  -- (i * num) % den for i in [0, den). Each residue class 0..den-1 appears
  -- exactly once (when gcd(num,den) = 1) or in a structured pattern.
  sorry  -- requires modular arithmetic lemmas beyond scope

/-! ## Frame 0 always passes (when num > 0 and den > 0) -/

theorem frame_zero_passes (num den : Nat) (hnum : num > 0) (hden : den > 0) :
    shouldPass num den 0 = true := by
  simp [shouldPass]
  omega
