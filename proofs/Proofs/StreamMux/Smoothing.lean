-- Proofs/StreamMux/Smoothing.lean: Correctness proofs for inertial smoothing
-- and FPS fraction packing

import Spec.StreamMux.Smoothing

open StreamMux

/-! ## Inertial Smoothing — count=0 yields new value -/

/-- When count=0, effectiveInertia=0, so result = new.
    weightOld = inertiaNum * 0 = 0
    weightNew = inertiaDen * 3 - 0 = inertiaDen * 3 = smoothedDen
    smoothedNum = old * 0 + new * smoothedDen = new * smoothedDen. -/
theorem smoothed_count_zero (old new_ inertiaNum inertiaDen : Nat) :
    smoothedNum old new_ inertiaNum inertiaDen 0 = new_ * smoothedDen inertiaDen 0 := by
  simp [smoothedNum, smoothedDen, weightOld, weightNew]

/-! ## Inertial Smoothing — inertia=0 yields new value -/

/-- When inertiaNum=0, effectiveInertia=0, so result = new. -/
theorem smoothed_inertia_zero (old new_ inertiaDen count : Nat) :
    smoothedNum old new_ 0 inertiaDen count = new_ * smoothedDen inertiaDen count := by
  simp [smoothedNum, smoothedDen, weightOld, weightNew]

/-! ## Weights sum to denominator -/

/-- The weights sum to the common denominator when the effective inertia is valid. -/
theorem weights_sum (inertiaNum inertiaDen count : Nat)
    (hValid : inertiaNum * count ≤ inertiaDen * (count + 3)) :
    weightOld inertiaNum count + weightNew inertiaNum inertiaDen count =
    smoothedDen inertiaDen count := by
  simp only [weightOld, weightNew, smoothedDen]
  omega

/-! ## Inertial Smoothing — convex combination (result between old and new)

  We prove: min(old, new) * den ≤ smoothedNum ≤ max(old, new) * den.

  The smoothedNum = old * wO + new * wN where wO + wN = den.
  For min bound: min(old,new) * (wO + wN) ≤ old * wO + new * wN
  For max bound: old * wO + new * wN ≤ max(old,new) * (wO + wN) -/

/-- The smoothed value is at least min(old, new) * den. -/
theorem smoothed_ge_min (old new_ inertiaNum inertiaDen count : Nat)
    (hValid : inertiaNum * count ≤ inertiaDen * (count + 3)) :
    min old new_ * smoothedDen inertiaDen count ≤
    smoothedNum old new_ inertiaNum inertiaDen count := by
  simp only [smoothedNum, smoothedDen, weightOld, weightNew]
  by_cases h : old ≤ new_
  · simp [Nat.min_eq_left h]
    -- Need: old * (inertiaDen * (count + 3))
    --     ≤ old * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count)
    -- Since old ≤ new_ and both multiply non-negative weights:
    -- old * d = old * e + old * (d - e) ≤ old * e + new_ * (d - e)
    -- where e = inertiaNum * count, d = inertiaDen * (count + 3)
    have key : old * (inertiaDen * (count + 3) - inertiaNum * count) ≤
               new_ * (inertiaDen * (count + 3) - inertiaNum * count) :=
      Nat.mul_le_mul_right _ h
    have split_eq : old * (inertiaDen * (count + 3)) =
        old * (inertiaNum * count) + old * (inertiaDen * (count + 3) - inertiaNum * count) := by
      have := Nat.add_sub_cancel' hValid
      calc old * (inertiaDen * (count + 3))
          = old * (inertiaNum * count + (inertiaDen * (count + 3) - inertiaNum * count)) := by
            congr 1; omega
        _ = old * (inertiaNum * count) + old * (inertiaDen * (count + 3) - inertiaNum * count) :=
            Nat.left_distrib old _ _
    rw [split_eq]
    exact Nat.add_le_add_left key _
  · have hLe : new_ ≤ old := by omega
    simp [Nat.min_eq_right hLe]
    -- Need: new_ * d ≤ old * e + new_ * (d - e)
    -- new_ * d = new_ * e + new_ * (d - e) ≤ old * e + new_ * (d - e)
    have key : new_ * (inertiaNum * count) ≤ old * (inertiaNum * count) :=
      Nat.mul_le_mul_right _ hLe
    have split_eq : new_ * (inertiaDen * (count + 3)) =
        new_ * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count) := by
      calc new_ * (inertiaDen * (count + 3))
          = new_ * (inertiaNum * count + (inertiaDen * (count + 3) - inertiaNum * count)) := by
            congr 1; omega
        _ = new_ * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count) :=
            Nat.left_distrib new_ _ _
    rw [split_eq]
    exact Nat.add_le_add_right key _

/-- The smoothed value is at most max(old, new) * den. -/
theorem smoothed_le_max (old new_ inertiaNum inertiaDen count : Nat)
    (hValid : inertiaNum * count ≤ inertiaDen * (count + 3)) :
    smoothedNum old new_ inertiaNum inertiaDen count ≤
    max old new_ * smoothedDen inertiaDen count := by
  simp only [smoothedNum, smoothedDen, weightOld, weightNew]
  by_cases h : old ≤ new_
  · simp [Nat.max_eq_right h]
    -- Need: old * e + new_ * (d - e) ≤ new_ * d
    -- old * e ≤ new_ * e, so old * e + new_ * (d - e) ≤ new_ * e + new_ * (d - e) = new_ * d
    have key : old * (inertiaNum * count) ≤ new_ * (inertiaNum * count) :=
      Nat.mul_le_mul_right _ h
    have join_eq : new_ * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count) =
        new_ * (inertiaDen * (count + 3)) := by
      calc new_ * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count)
          = new_ * (inertiaNum * count + (inertiaDen * (count + 3) - inertiaNum * count)) :=
            (Nat.left_distrib new_ _ _).symm
        _ = new_ * (inertiaDen * (count + 3)) := by congr 1; omega
    calc old * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count)
        ≤ new_ * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count) :=
          Nat.add_le_add_right key _
      _ = new_ * (inertiaDen * (count + 3)) := join_eq
  · have hLe : new_ ≤ old := by omega
    simp [Nat.max_eq_left hLe]
    -- Need: old * e + new_ * (d - e) ≤ old * d
    -- new_ * (d - e) ≤ old * (d - e), so old * e + new_ * (d - e) ≤ old * e + old * (d - e) = old * d
    have key : new_ * (inertiaDen * (count + 3) - inertiaNum * count) ≤
               old * (inertiaDen * (count + 3) - inertiaNum * count) :=
      Nat.mul_le_mul_right _ hLe
    have join_eq : old * (inertiaNum * count) + old * (inertiaDen * (count + 3) - inertiaNum * count) =
        old * (inertiaDen * (count + 3)) := by
      calc old * (inertiaNum * count) + old * (inertiaDen * (count + 3) - inertiaNum * count)
          = old * (inertiaNum * count + (inertiaDen * (count + 3) - inertiaNum * count)) :=
            (Nat.left_distrib old _ _).symm
        _ = old * (inertiaDen * (count + 3)) := by congr 1; omega
    calc old * (inertiaNum * count) + new_ * (inertiaDen * (count + 3) - inertiaNum * count)
        ≤ old * (inertiaNum * count) + old * (inertiaDen * (count + 3) - inertiaNum * count) :=
          Nat.add_le_add_left key _
      _ = old * (inertiaDen * (count + 3)) := join_eq

/-! ## Inertial Smoothing — increasing count increases effective inertia

  effectiveInertia = inertiaNum * count / (inertiaDen * (count + 3))

  We prove: count1 ≤ count2 →
    effNum(count1) * effDen(count2) ≤ effNum(count2) * effDen(count1)

  This reduces to: count1 * (count2 + 3) ≤ count2 * (count1 + 3)
  ↔ count1*count2 + 3*count1 ≤ count2*count1 + 3*count2
  ↔ 3*count1 ≤ 3*count2. -/

/-- Helper: the core count fraction inequality. -/
private theorem count_frac_le (c1 c2 : Nat) (h : c1 ≤ c2) :
    c1 * (c2 + 3) ≤ c2 * (c1 + 3) := by
  -- c1*(c2+3) = c1*c2 + 3*c1, c2*(c1+3) = c2*c1 + 3*c2
  -- Suffices: 3*c1 ≤ 3*c2, which follows from c1 ≤ c2
  have lhs : c1 * (c2 + 3) = c1 * c2 + c1 * 3 := Nat.left_distrib c1 c2 3
  have rhs : c2 * (c1 + 3) = c2 * c1 + c2 * 3 := Nat.left_distrib c2 c1 3
  have comm : c1 * c2 = c2 * c1 := Nat.mul_comm c1 c2
  rw [lhs, rhs, comm]
  exact Nat.add_le_add_left (Nat.mul_le_mul_right 3 h) _

/-- Increasing count increases effective inertia (as a cross-multiplied fraction). -/
theorem effective_inertia_monotone (inertiaNum inertiaDen count1 count2 : Nat)
    (hLe : count1 ≤ count2) :
    effectiveInertiaNum inertiaNum count1 * effectiveInertiaDen inertiaDen count2 ≤
    effectiveInertiaNum inertiaNum count2 * effectiveInertiaDen inertiaDen count1 := by
  simp only [effectiveInertiaNum, effectiveInertiaDen]
  -- Need: (inertiaNum * count1) * (inertiaDen * (count2 + 3))
  --     ≤ (inertiaNum * count2) * (inertiaDen * (count1 + 3))
  -- Rearrange both sides to inertiaNum * inertiaDen * (countI * (countJ + 3))
  have lhs_eq : inertiaNum * count1 * (inertiaDen * (count2 + 3)) =
      inertiaNum * inertiaDen * (count1 * (count2 + 3)) := by
    calc inertiaNum * count1 * (inertiaDen * (count2 + 3))
        = inertiaNum * (count1 * (inertiaDen * (count2 + 3))) := Nat.mul_assoc _ _ _
      _ = inertiaNum * (inertiaDen * (count1 * (count2 + 3))) := by
          congr 1; calc count1 * (inertiaDen * (count2 + 3))
              = (count1 * inertiaDen) * (count2 + 3) := (Nat.mul_assoc _ _ _).symm
            _ = (inertiaDen * count1) * (count2 + 3) := by rw [Nat.mul_comm count1 inertiaDen]
            _ = inertiaDen * (count1 * (count2 + 3)) := Nat.mul_assoc _ _ _
      _ = (inertiaNum * inertiaDen) * (count1 * (count2 + 3)) := (Nat.mul_assoc _ _ _).symm
  have rhs_eq : inertiaNum * count2 * (inertiaDen * (count1 + 3)) =
      inertiaNum * inertiaDen * (count2 * (count1 + 3)) := by
    calc inertiaNum * count2 * (inertiaDen * (count1 + 3))
        = inertiaNum * (count2 * (inertiaDen * (count1 + 3))) := Nat.mul_assoc _ _ _
      _ = inertiaNum * (inertiaDen * (count2 * (count1 + 3))) := by
          congr 1; calc count2 * (inertiaDen * (count1 + 3))
              = (count2 * inertiaDen) * (count1 + 3) := (Nat.mul_assoc _ _ _).symm
            _ = (inertiaDen * count2) * (count1 + 3) := by rw [Nat.mul_comm count2 inertiaDen]
            _ = inertiaDen * (count2 * (count1 + 3)) := Nat.mul_assoc _ _ _
      _ = (inertiaNum * inertiaDen) * (count2 * (count1 + 3)) := (Nat.mul_assoc _ _ _).symm
  rw [lhs_eq, rhs_eq]
  exact Nat.mul_le_mul_left _ (count_frac_le count1 count2 hLe)

/-! ## FPS Fraction Packing — roundtrip -/

/-- Unpacking the numerator from a packed value recovers the original numerator,
    provided both num and den fit in 32 bits. -/
theorem unpack_pack_num (num den : Nat)
    (_hNum : num < 2^32) (hDen : den < 2^32) :
    unpackFPSNum (packFPS num den) = num := by
  simp only [unpackFPSNum, packFPS]
  omega

/-- Unpacking the denominator from a packed value recovers the original denominator,
    provided den fits in 32 bits. -/
theorem unpack_pack_den (num den : Nat)
    (_hNum : num < 2^32) (hDen : den < 2^32) :
    unpackFPSDen (packFPS num den) = den := by
  simp only [unpackFPSDen, packFPS]
  omega

/-- Full roundtrip: unpack(pack(num, den)) = (num, den). -/
theorem fps_roundtrip (num den : Nat)
    (hNum : num < 2^32) (hDen : den < 2^32) :
    unpackFPSNum (packFPS num den) = num ∧
    unpackFPSDen (packFPS num den) = den :=
  ⟨unpack_pack_num num den hNum hDen, unpack_pack_den num den hNum hDen⟩

/-! ## FPS Fraction Packing — injectivity -/

/-- Pack is injective: if pack(a,b) = pack(c,d) and all values < 2^32,
    then a = c and b = d. -/
theorem pack_injective (a b c d : Nat)
    (_hA : a < 2^32) (hB : b < 2^32)
    (_hC : c < 2^32) (hD : d < 2^32)
    (hEq : packFPS a b = packFPS c d) :
    a = c ∧ b = d := by
  simp only [packFPS] at hEq
  constructor
  · omega
  · omega
