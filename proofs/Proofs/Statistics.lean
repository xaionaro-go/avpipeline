-- Proofs/Statistics.lean: Correctness proofs for statistics counters

import Spec.Statistics

open CountersItem CountersSubSection MediaType

/-! ## Total = sum of fields -/

/-- TotalCount is the sum of all four count fields. -/
theorem totalCount_eq_sum (s : CountersSubSection) :
    s.totalCount = s.video.count + s.audio.count + s.other.count + s.unknown.count := by
  rfl

/-- TotalBytes is the sum of all four bytes fields. -/
theorem totalBytes_eq_sum (s : CountersSubSection) :
    s.totalBytes = s.video.bytes + s.audio.bytes + s.other.bytes + s.unknown.bytes := by
  rfl

/-! ## Increment isolation: each increment only changes its target field -/

/-- Incrementing Video only changes the video field. -/
theorem increment_video_only_changes_video (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment video sz
    s'.audio = s.audio ∧ s'.other = s.other ∧ s'.unknown = s.unknown := by
  simp [CountersSubSection.increment]

/-- Incrementing Audio only changes the audio field. -/
theorem increment_audio_only_changes_audio (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment audio sz
    s'.video = s.video ∧ s'.other = s.other ∧ s'.unknown = s.unknown := by
  simp [CountersSubSection.increment]

/-- Incrementing Other only changes the other field. -/
theorem increment_other_only_changes_other (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment other sz
    s'.video = s.video ∧ s'.audio = s.audio ∧ s'.unknown = s.unknown := by
  simp [CountersSubSection.increment]

/-- Incrementing Unknown routes to Other (Go default branch), leaving
    video, audio, and unknown unchanged. -/
theorem increment_unknown_only_changes_other (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment unknown sz
    s'.video = s.video ∧ s'.audio = s.audio ∧ s'.unknown = s.unknown := by
  simp [CountersSubSection.increment]

/-! ## Increment correctly updates the target field -/

/-- Incrementing Video adds 1 to video.count and msgSize to video.bytes. -/
theorem increment_video_updates (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment video sz
    s'.video.count = s.video.count + 1 ∧ s'.video.bytes = s.video.bytes + sz := by
  simp [CountersSubSection.increment, CountersItem.increment]

/-- Incrementing Audio adds 1 to audio.count and msgSize to audio.bytes. -/
theorem increment_audio_updates (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment audio sz
    s'.audio.count = s.audio.count + 1 ∧ s'.audio.bytes = s.audio.bytes + sz := by
  simp [CountersSubSection.increment, CountersItem.increment]

/-- Incrementing Other adds 1 to other.count and msgSize to other.bytes. -/
theorem increment_other_updates (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment other sz
    s'.other.count = s.other.count + 1 ∧ s'.other.bytes = s.other.bytes + sz := by
  simp [CountersSubSection.increment, CountersItem.increment]

/-- Incrementing Unknown routes to Other: adds 1 to other.count and msgSize
    to other.bytes, while unknown is untouched. -/
theorem increment_unknown_updates_other (s : CountersSubSection) (sz : Nat) :
    let s' := s.increment unknown sz
    s'.other.count = s.other.count + 1 ∧ s'.other.bytes = s.other.bytes + sz ∧
    s'.unknown = s.unknown := by
  simp [CountersSubSection.increment, CountersItem.increment]

/-! ## Zero-initialized total = 0 -/

/-- A zero-initialized subsection has totalCount = 0. -/
theorem zero_totalCount : CountersSubSection.zero.totalCount = 0 := by
  rfl

/-- A zero-initialized subsection has totalBytes = 0. -/
theorem zero_totalBytes : CountersSubSection.zero.totalBytes = 0 := by
  rfl

/-! ## Increment commutativity: total is order-independent -/

/-- Total count after incrementing with type A then B equals total after B then A. -/
theorem increment_commutative_totalCount
    (s : CountersSubSection) (mt1 mt2 : MediaType) (sz1 sz2 : Nat) :
    (s.increment mt1 sz1 |>.increment mt2 sz2).totalCount =
    (s.increment mt2 sz2 |>.increment mt1 sz1).totalCount := by
  cases mt1 <;> cases mt2 <;> simp [CountersSubSection.increment, CountersSubSection.totalCount, CountersItem.increment] <;> omega

/-- Total bytes after incrementing with type A then B equals total after B then A. -/
theorem increment_commutative_totalBytes
    (s : CountersSubSection) (mt1 mt2 : MediaType) (sz1 sz2 : Nat) :
    (s.increment mt1 sz1 |>.increment mt2 sz2).totalBytes =
    (s.increment mt2 sz2 |>.increment mt1 sz1).totalBytes := by
  cases mt1 <;> cases mt2 <;> simp [CountersSubSection.increment, CountersSubSection.totalBytes, CountersItem.increment] <;> omega

/-! ## Each field >= 0 (Nat guarantees) -/

/-- All count fields are non-negative (trivially true for Nat). -/
theorem fields_nonneg (s : CountersSubSection) :
    s.video.count ≥ 0 ∧ s.audio.count ≥ 0 ∧
    s.other.count ≥ 0 ∧ s.unknown.count ≥ 0 := by
  exact ⟨Nat.zero_le _, Nat.zero_le _, Nat.zero_le _, Nat.zero_le _⟩

/-- All bytes fields are non-negative (trivially true for Nat). -/
theorem bytes_nonneg (s : CountersSubSection) :
    s.video.bytes ≥ 0 ∧ s.audio.bytes ≥ 0 ∧
    s.other.bytes ≥ 0 ∧ s.unknown.bytes ≥ 0 := by
  exact ⟨Nat.zero_le _, Nat.zero_le _, Nat.zero_le _, Nat.zero_le _⟩

/-! ## After n increments of same type, that field's count = n -/

/-- Helper: apply n increments of the same media type. -/
def incrementN (s : CountersSubSection) (mt : MediaType) (sz : Nat) : Nat → CountersSubSection
  | 0     => s
  | n + 1 => (incrementN s mt sz n).increment mt sz

/-- After n video increments from zero, video.count = n. -/
theorem n_increments_video_count (n : Nat) (sz : Nat) :
    (incrementN CountersSubSection.zero video sz n).video.count = n := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp [incrementN, CountersSubSection.increment, CountersItem.increment, ih]

/-- After n audio increments from zero, audio.count = n. -/
theorem n_increments_audio_count (n : Nat) (sz : Nat) :
    (incrementN CountersSubSection.zero audio sz n).audio.count = n := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp [incrementN, CountersSubSection.increment, CountersItem.increment, ih]

/-- After n other increments from zero, other.count = n. -/
theorem n_increments_other_count (n : Nat) (sz : Nat) :
    (incrementN CountersSubSection.zero other sz n).other.count = n := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp [incrementN, CountersSubSection.increment, CountersItem.increment, ih]

/-- After n unknown-type increments from zero, other.count = n
    (unknown routes to other via the default branch in Go's Get). -/
theorem n_increments_unknown_routes_to_other (n : Nat) (sz : Nat) :
    (incrementN CountersSubSection.zero unknown sz n).other.count = n := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp [incrementN, CountersSubSection.increment, CountersItem.increment, ih]

/-! ## Increment increases totalCount by exactly 1 -/

/-- Any single increment increases totalCount by exactly 1. -/
theorem increment_totalCount_plus_one (s : CountersSubSection) (mt : MediaType) (sz : Nat) :
    (s.increment mt sz).totalCount = s.totalCount + 1 := by
  cases mt <;> simp [CountersSubSection.increment, CountersSubSection.totalCount, CountersItem.increment] <;> omega

/-! ## Increment increases totalBytes by exactly msgSize -/

/-- Any single increment increases totalBytes by exactly msgSize. -/
theorem increment_totalBytes_plus_msgSize (s : CountersSubSection) (mt : MediaType) (sz : Nat) :
    (s.increment mt sz).totalBytes = s.totalBytes + sz := by
  cases mt <;> simp [CountersSubSection.increment, CountersSubSection.totalBytes, CountersItem.increment] <;> omega

/-! ## After n increments from zero, totalCount = n -/

/-- After n increments of any single type from zero, totalCount = n. -/
theorem n_increments_totalCount (n : Nat) (mt : MediaType) (sz : Nat) :
    (incrementN CountersSubSection.zero mt sz n).totalCount = n := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp [incrementN, increment_totalCount_plus_one, ih]

/-! ## CountersItem properties -/

/-- Zero item has count 0 and bytes 0. -/
theorem item_zero_values : CountersItem.zero.count = 0 ∧ CountersItem.zero.bytes = 0 := by
  exact ⟨rfl, rfl⟩

/-- Increment adds 1 to count. -/
theorem item_increment_count (c : CountersItem) (sz : Nat) :
    (c.increment sz).count = c.count + 1 := by
  rfl

/-- Increment adds msgSize to bytes. -/
theorem item_increment_bytes (c : CountersItem) (sz : Nat) :
    (c.increment sz).bytes = c.bytes + sz := by
  rfl

/-! ## Routing correctness: Get returns the correct field -/

/-- Get video returns the video field. -/
theorem get_video (s : CountersSubSection) : s.get video = s.video := by rfl

/-- Get audio returns the audio field. -/
theorem get_audio (s : CountersSubSection) : s.get audio = s.audio := by rfl

/-- Get other returns the other field. -/
theorem get_other (s : CountersSubSection) : s.get other = s.other := by rfl

/-- Get unknown returns the other field (default routing in Go). -/
theorem get_unknown (s : CountersSubSection) : s.get unknown = s.other := by rfl
