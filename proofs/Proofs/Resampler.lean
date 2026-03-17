-- Proofs/Resampler.lean: Correctness proofs for audio resampler

import Spec.Resampler

open AudioFifo

/-! ## Ceiling division properties -/

/-- Ceiling division never underestimates: ceilDiv a b * b ≥ a for b > 0. -/
theorem ceilDiv_mul_ge (a b : Nat) (hb : b > 0) : ceilDiv a b * b ≥ a := by
  unfold ceilDiv
  simp [Nat.pos_iff_ne_zero.mp hb]
  have hmod := Nat.mod_lt (a + b - 1) hb
  have hdivmod := Nat.div_add_mod (a + b - 1) b
  rw [Nat.mul_comm]
  omega

/-- Ceiling division is at least as large as floor division. -/
theorem ceilDiv_ge_div (a b : Nat) (hb : b > 0) : ceilDiv a b ≥ a / b := by
  unfold ceilDiv
  simp [Nat.pos_iff_ne_zero.mp hb]
  rw [Nat.div_le_iff_le_mul hb]
  have hmod := Nat.mod_lt (a + b - 1) hb
  have hdivmod := Nat.div_add_mod (a + b - 1) b
  have h_comm : (a + b - 1) / b * b = b * ((a + b - 1) / b) := Nat.mul_comm _ _
  omega

/-- ceilDiv a b ≥ 1 when a > 0 and b > 0. -/
theorem ceilDiv_pos (a b : Nat) (ha : a > 0) (hb : b > 0) : ceilDiv a b ≥ 1 := by
  unfold ceilDiv
  simp [Nat.pos_iff_ne_zero.mp hb]
  exact Nat.le_div_iff_mul_le hb |>.mpr (by omega)

/-- ceilDiv a a = 1 for a > 0. -/
theorem ceilDiv_self (a : Nat) (ha : a > 0) : ceilDiv a a = 1 := by
  unfold ceilDiv
  simp [Nat.pos_iff_ne_zero.mp ha]
  have h1 : 1 ≤ (a + a - 1) / a :=
    (Nat.le_div_iff_mul_le ha).mpr (by omega)
  have h2 : (a + a - 1) / a < 2 :=
    (Nat.div_lt_iff_lt_mul ha).mpr (by omega)
  omega

/-- ceilDiv 0 b = 0. -/
theorem ceilDiv_zero_left (b : Nat) : ceilDiv 0 b = 0 := by
  unfold ceilDiv
  if hb : b = 0 then
    simp [hb]
  else
    simp [hb]
    exact Nat.div_eq_of_lt (by omega)

/-! ## expectedOutputSamples properties -/

/-- Output samples ≥ chunkSize (always, no preconditions needed). -/
theorem output_ge_chunkSize (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat) :
    expectedOutputSamples fmt inRate inputSamples ≥ fmt.chunkSize := by
  simp [expectedOutputSamples]

/-- Output samples ≥ 1 when output rate > 0, input > 0, and input rate > 0. -/
theorem output_ge_one (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat)
    (hInput : inputSamples > 0) (hRate : inRate > 0)
    (hOutRate : fmt.sampleRate > 0) :
    expectedOutputSamples fmt inRate inputSamples ≥ 1 := by
  simp [expectedOutputSamples]
  have hProd : inputSamples * fmt.sampleRate > 0 := Nat.mul_pos hInput hOutRate
  have h : ceilDiv (inputSamples * fmt.sampleRate) inRate ≥ 1 := by
    unfold ceilDiv
    simp [Nat.pos_iff_ne_zero.mp hRate]
    exact (Nat.le_div_iff_mul_le hRate).mpr (by omega)
  omega

/-- Output samples ≥ 1 when chunkSize > 0 (no other preconditions needed). -/
theorem output_ge_one_chunk (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat)
    (hChunk : fmt.chunkSize > 0) :
    expectedOutputSamples fmt inRate inputSamples ≥ 1 := by
  simp [expectedOutputSamples]
  omega

/-- Output samples ≥ 2 * chunkSize when chunkSize > 0
    (because result = max(ceil, chunkSize) + chunkSize ≥ chunkSize + chunkSize). -/
theorem output_ge_double_chunk (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat)
    (_ : fmt.chunkSize > 0) :
    expectedOutputSamples fmt inRate inputSamples ≥ 2 * fmt.chunkSize := by
  simp [expectedOutputSamples]
  omega

/-- When input rate = output rate, output ≥ inputSamples. -/
theorem output_ge_input_same_rate (fmt : ResamplerFormat) (inputSamples : Nat)
    (hInput : inputSamples > 0) (hRate : fmt.sampleRate > 0) :
    expectedOutputSamples fmt fmt.sampleRate inputSamples ≥ inputSamples := by
  simp [expectedOutputSamples]
  have : ceilDiv (inputSamples * fmt.sampleRate) fmt.sampleRate ≥ inputSamples := by
    unfold ceilDiv
    simp [Nat.pos_iff_ne_zero.mp hRate]
    exact (Nat.le_div_iff_mul_le hRate).mpr (by omega)
  omega

/-- The proportionality bound: outSamples ≥ ceil(inputSamples * outRate / inRate). -/
theorem output_ge_ceil_ratio (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat) :
    expectedOutputSamples fmt inRate inputSamples ≥
      ceilDiv (inputSamples * fmt.sampleRate) inRate := by
  simp [expectedOutputSamples]
  omega

/-- Output with chunkSize = 0 is exactly the ceiling ratio. -/
theorem output_zero_chunk (inRate : Nat) (outRate : Nat) (inputSamples : Nat) :
    expectedOutputSamples ⟨outRate, 0⟩ inRate inputSamples =
      ceilDiv (inputSamples * outRate) inRate := by
  simp [expectedOutputSamples]

/-! ## FIFO properties -/

/-- Empty FIFO has size 0. -/
theorem empty_size : (AudioFifo.empty : AudioFifo α).size = 0 := by
  simp [AudioFifo.empty, AudioFifo.size]

/-- Writing n items to empty FIFO gives size n. -/
theorem write_empty_size (samples : List α) :
    (AudioFifo.empty.write samples).size = samples.length := by
  simp [AudioFifo.empty, AudioFifo.write, AudioFifo.size]

/-- Writing preserves existing data: size increases by the number of written samples. -/
theorem write_size (fifo : AudioFifo α) (samples : List α) :
    (fifo.write samples).size = fifo.size + samples.length := by
  simp [AudioFifo.write, AudioFifo.size, List.length_append]

/-- Write then read back gets the original samples. -/
theorem write_read_identity (samples : List α) :
    (AudioFifo.empty.write samples).read samples.length =
      (samples, AudioFifo.empty) := by
  simp [AudioFifo.empty, AudioFifo.write, AudioFifo.read, AudioFifo.size]

/-- Read n from FIFO with n items gets all items. -/
theorem read_all (fifo : AudioFifo α) (n : Nat) (h : fifo.size = n) :
    (fifo.read n).1 = fifo.data := by
  simp [AudioFifo.read, AudioFifo.size] at *
  exact List.take_of_length_le (by omega)

/-- After reading all, remaining FIFO is empty. -/
theorem read_all_remaining (fifo : AudioFifo α) :
    (fifo.read fifo.size).2 = AudioFifo.empty := by
  simp [AudioFifo.read, AudioFifo.size, AudioFifo.empty]

/-- Read 0 returns empty list and unchanged FIFO. -/
theorem read_zero (fifo : AudioFifo α) :
    (fifo.read 0).1 = [] ∧ (fifo.read 0).2 = fifo := by
  simp [AudioFifo.read]

/-- After write then read of the same data, remaining size is original size. -/
theorem write_read_remaining_size (fifo : AudioFifo α) (samples : List α) :
    ((fifo.write samples).read samples.length).2.size = fifo.size := by
  simp [AudioFifo.write, AudioFifo.read, AudioFifo.size, List.length_append]

/-! ## FIFO readWithThreshold properties -/

/-- Empty FIFO returns EOF. -/
theorem readWithThreshold_empty (minSize readCount : Nat) :
    (AudioFifo.empty : AudioFifo α).readWithThreshold minSize readCount = .eof := by
  simp [AudioFifo.readWithThreshold, AudioFifo.empty, AudioFifo.size]

/-- FIFO with less than minSize returns EAGAIN (when non-empty). -/
theorem readWithThreshold_eagain (fifo : AudioFifo α)
    (hNonEmpty : fifo.size > 0) (hSmall : fifo.size < minSize) (readCount : Nat) :
    fifo.readWithThreshold minSize readCount = .eagain := by
  unfold AudioFifo.readWithThreshold
  simp [AudioFifo.size] at *
  split
  · omega
  · rfl

/-- FIFO with enough data returns ok. -/
theorem readWithThreshold_ok (fifo : AudioFifo α)
    (hEnough : fifo.size ≥ minSize) (hNonEmpty : fifo.size > 0) (readCount : Nat) :
    ∃ samples rest, fifo.readWithThreshold minSize readCount = .ok samples rest := by
  unfold AudioFifo.readWithThreshold
  simp [AudioFifo.size] at *
  split
  · omega
  · split
    · omega
    · exact ⟨_, _, rfl⟩

/-! ## Format change detection -/

/-- No previous format always succeeds. -/
theorem checkFormat_none (curr : PCMFormat) :
    checkFormat none curr = .ok := by
  simp [checkFormat]

/-- Same format succeeds. -/
theorem checkFormat_same (fmt : PCMFormat) :
    checkFormat (some fmt) fmt = .ok := by
  simp [checkFormat]

/-- Different format is detected as a format change. -/
theorem checkFormat_changed (prev curr : PCMFormat) (hNeq : prev ≠ curr) :
    checkFormat (some prev) curr = .formatChanged := by
  unfold checkFormat
  simp
  exact hNeq

/-! ## FIFO write-read round-trip -/

/-- Write n items to empty FIFO then read n = get same items back. -/
theorem fifo_write_read_roundtrip (samples : List α) :
    let fifo := AudioFifo.empty.write samples
    let (got, _) := fifo.read samples.length
    got = samples := by
  simp [AudioFifo.empty, AudioFifo.write, AudioFifo.read]

/-- FIFO capacity after write is sufficient: after writing n items, size ≥ n. -/
theorem fifo_capacity_after_write (fifo : AudioFifo α) (samples : List α) :
    (fifo.write samples).size ≥ samples.length := by
  simp [AudioFifo.write, AudioFifo.size, List.length_append]
