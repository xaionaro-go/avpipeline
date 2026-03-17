-- Proofs/Units.lean: Correctness proofs for unit conversions

import Spec.Units

open Units

/-! ## Bytes ↔ Bits: tob/toB roundtrip -/

/-- `tob(x) = 8 * x` — bits are always 8× bytes. -/
theorem tob_eq_8_mul (x : UB) :
    (tob x).val = 8 * x.val := by
  simp [tob, Int.mul_comm]

/-- `tobps(x) = 8 * x` — bps are always 8× Bps. -/
theorem tobps_eq_8_mul (x : UBps) :
    (tobps x).val = 8 * x.val := by
  simp [tobps, Int.mul_comm]

/-- `toB(tob(x)) = x` — Bytes→bits→Bytes roundtrip is identity.
    No divisibility precondition needed because tob always produces a multiple of 8. -/
theorem toB_tob_roundtrip (x : UB) :
    toB (tob x) = x := by
  simp [toB, tob]

/-- `toBps(tobps(x)) = x` — Bps→bps→Bps roundtrip is identity. -/
theorem toBps_tobps_roundtrip (x : UBps) :
    toBps (tobps x) = x := by
  simp [toBps, tobps]

/-- `tob(toB(v)) = v` when v is divisible by 8. -/
theorem tob_toB_roundtrip (v : Ub) (h : 8 ∣ v.val) :
    tob (toB v) = v := by
  obtain ⟨k, hk⟩ := h
  cases v with | mk val =>
  simp only [Ub.mk.injEq] at hk
  subst hk
  simp [tob, toB]
  omega

/-- `tobps(toBps(v)) = v` when v is divisible by 8. -/
theorem tobps_toBps_roundtrip (v : Ubps) (h : 8 ∣ v.val) :
    tobps (toBps v) = v := by
  obtain ⟨k, hk⟩ := h
  cases v with | mk val =>
  simp only [Ubps.mk.injEq] at hk
  subst hk
  simp [tobps, toBps]
  omega

/-! ## Rate × Time roundtrip consistency -/

/-- Rate × Time → Amount → Rate roundtrip: recovering rate from (rate × time) / time. -/
theorem bytes_rate_time_roundtrip (r : UBps) (t : US) (ht : t.val ≠ 0) :
    bytesToRate (rateTimeToBytes r t) t = r := by
  ext
  simp [bytesToRate, rateTimeToBytes]
  exact Int.mul_ediv_cancel r.val ht

/-- Rate × Time → Amount → Time roundtrip: recovering time from (rate × time) / rate. -/
theorem bytes_time_rate_roundtrip (r : UBps) (t : US) (hr : r.val ≠ 0) :
    bytesToTime (rateTimeToBytes r t) r = t := by
  ext
  simp [bytesToTime, rateTimeToBytes, Int.mul_comm r.val t.val]
  exact Int.mul_ediv_cancel t.val hr

/-- Bits variant: rate × time → amount → rate roundtrip. -/
theorem bits_rate_time_roundtrip (r : Ubps) (t : US) (ht : t.val ≠ 0) :
    bitsToRate (rateTimeToBits r t) t = r := by
  ext
  simp [bitsToRate, rateTimeToBits]
  exact Int.mul_ediv_cancel r.val ht

/-- Bits variant: rate × time → amount → time roundtrip. -/
theorem bits_time_rate_roundtrip (r : Ubps) (t : US) (hr : r.val ≠ 0) :
    bitsToTime (rateTimeToBits r t) r = t := by
  ext
  simp [bitsToTime, rateTimeToBits, Int.mul_comm r.val t.val]
  exact Int.mul_ediv_cancel t.val hr

/-! ## Cross-domain consistency: Byte/Bit rate conversions compose with rate×time -/

/-- Computing amount via bits yields 8× the byte amount. -/
theorem bits_amount_eq_8_bytes_amount (r : UBps) (t : US) :
    (rateTimeToBits (tobps r) t).val = 8 * (rateTimeToBytes r t).val := by
  simp only [rateTimeToBits, tobps, rateTimeToBytes]
  rw [show r.val * 8 * t.val = r.val * t.val * 8 from by
    rw [Int.mul_assoc, Int.mul_comm 8 t.val, ← Int.mul_assoc]]
  exact Int.mul_comm (r.val * t.val) 8

/-- Converting byte-rate to bit-rate then computing time gives the same time as computing
    directly with byte-rate. -/
theorem cross_domain_time_consistent (v : UB) (r : UBps) :
    bitsToTime (tob v) (tobps r) = bytesToTime v r := by
  simp [bitsToTime, tob, tobps, bytesToTime]
