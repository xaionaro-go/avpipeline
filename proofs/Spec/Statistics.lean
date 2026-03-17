-- Spec/Statistics.lean: Formal specification of statistics counters
-- from types/statistics.go

/-!
  Models the per-media-type counter subsection (`CountersSubSection`)
  and its item (`CountersItem`) from types/statistics.go.

  Go uses `atomic.Uint64` for thread safety; we model plain `Nat` values
  since atomics do not affect functional semantics.

  The routing logic in `CountersSubSection.Get` maps:
  - MediaTypeVideo → Video field
  - MediaTypeAudio → Audio field
  - all others     → Other field
  The Unknown field is only accessed directly, never via Get/Increment.
-/

/-- Media types relevant to counter routing.
    Mirrors the subset of `MediaType` used by `CountersSubSection.Get`. -/
inductive MediaType where
  | video
  | audio
  | other
  | unknown
  deriving Repr, DecidableEq, Inhabited

/-- A single counter item tracking count and bytes.
    Mirrors `CountersItem` with `Count` and `Bytes` fields. -/
structure CountersItem where
  count : Nat
  bytes : Nat
  deriving Repr, DecidableEq

namespace CountersItem

/-- Zero-initialized counter item. Mirrors `NewCountersItem()`. -/
def zero : CountersItem := ⟨0, 0⟩

/-- Increment: adds 1 to count and msgSize to bytes.
    Mirrors `CountersItem.Increment(msgSize)`. -/
def increment (c : CountersItem) (msgSize : Nat) : CountersItem :=
  ⟨c.count + 1, c.bytes + msgSize⟩

end CountersItem

/-- Per-media-type counter subsection with four fields.
    Mirrors `CountersSubSection` from statistics.go. -/
structure CountersSubSection where
  video   : CountersItem
  audio   : CountersItem
  other   : CountersItem
  unknown : CountersItem
  deriving Repr, DecidableEq

namespace CountersSubSection

/-- Zero-initialized subsection. Mirrors `NewCountersSubSection()`. -/
def zero : CountersSubSection :=
  ⟨CountersItem.zero, CountersItem.zero, CountersItem.zero, CountersItem.zero⟩

/-- Route a media type to the correct field.
    Mirrors `CountersSubSection.Get(mediaType)`:
      Video → Video, Audio → Audio, default → Other.
    Note: Unknown field is not routed to by Get in Go. -/
def get (s : CountersSubSection) (mt : MediaType) : CountersItem :=
  match mt with
  | .video => s.video
  | .audio => s.audio
  | _      => s.other

/-- Update the field corresponding to a media type.
    Helper for expressing the increment operation. -/
def update (s : CountersSubSection) (mt : MediaType) (f : CountersItem → CountersItem) :
    CountersSubSection :=
  match mt with
  | .video   => { s with video   := f s.video }
  | .audio   => { s with audio   := f s.audio }
  | .other   => { s with other   := f s.other }
  | .unknown => { s with unknown := f s.unknown }

/-- Increment the counter for a given media type.
    Mirrors `CountersSubSection.Increment(mediaType, msgSize)`.
    Routes through Get, so video/audio go to their fields, rest to Other. -/
def increment (s : CountersSubSection) (mt : MediaType) (msgSize : Nat) :
    CountersSubSection :=
  match mt with
  | .video => { s with video := s.video.increment msgSize }
  | .audio => { s with audio := s.audio.increment msgSize }
  | _      => { s with other := s.other.increment msgSize }

/-- Total count across all four fields.
    Mirrors `CountersSubSection.TotalCount()`. -/
def totalCount (s : CountersSubSection) : Nat :=
  s.video.count + s.audio.count + s.other.count + s.unknown.count

/-- Total bytes across all four fields.
    Mirrors `CountersSubSection.TotalBytes()`. -/
def totalBytes (s : CountersSubSection) : Nat :=
  s.video.bytes + s.audio.bytes + s.other.bytes + s.unknown.bytes

end CountersSubSection
