-- Spec/Quality.lean: Formal specification of stream quality metrics from
-- packetorframe/filter/quality/stream_measurements.go

/-!
  Models `getStreamQualityLocked` as a pure function over a list of (DTS, Duration) pairs.
  Uses Int pairs (numerator, denominator) instead of Float to enable formal reasoning.

  The Go code iterates backward through a ring buffer, accumulating discontinuity
  and overlap lengths, then divides by the total interval.
-/

/-- Mirrors Go `types.StreamQuality` with rational fields (num/den). -/
structure StreamQuality where
  continuityNum : Int    -- numerator of continuity ratio
  continuityDen : Int    -- denominator (= interval)
  overlapNum    : Int    -- numerator of overlap ratio
  overlapDen    : Int    -- denominator (= interval)
  frameRateNum  : Int    -- numerator of frame rate
  frameRateDen  : Int    -- denominator (= interval in time units)
  invalidDTS    : Nat    -- count of invalid DTS entries
  deriving Repr, DecidableEq

/-- A single DTS+Duration entry, mirroring Go's `DTSAndDuration`. -/
structure DTSAndDuration where
  dts      : Int
  duration : Int
  deriving Repr, DecidableEq

namespace Quality

/--
  Accumulator for the backward scan loop.
  Mirrors the local variables in `getStreamQualityLocked`.
-/
structure LoopAcc where
  prevDTS             : Int   -- DTS of the previous (more recent) frame
  minDTS              : Int   -- smallest DTS seen so far
  discontinuityLength : Int   -- sum of positive gaps
  overlapLength       : Int   -- sum of absolute negative gaps
  frameCount          : Nat   -- number of accepted frames
  invalidDTS          : Nat   -- count of invalid DTS entries
  deriving Repr, DecidableEq

/--
  Process one item during the backward scan.
  Mirrors the loop body at lines 87-106 of stream_measurements.go.

  The Go code skips time-window filtering (dts < startTS) — we model the full list
  without a time cap, which makes the spec a superset.
-/
def processItem (acc : LoopAcc) (item : DTSAndDuration) : LoopAcc :=
  if item.dts >= acc.prevDTS then
    -- Invalid DTS: not strictly decreasing → skip, count as invalid
    { acc with invalidDTS := acc.invalidDTS + 1 }
  else
    let endTS := item.dts + item.duration
    let discontinuityRange := acc.prevDTS - endTS
    let newDiscontinuity :=
      if discontinuityRange > 0 then acc.discontinuityLength + discontinuityRange
      else acc.discontinuityLength
    let newOverlap :=
      if discontinuityRange < 0 then acc.overlapLength + (-discontinuityRange)
      else acc.overlapLength
    { prevDTS := item.dts
      minDTS := item.dts
      discontinuityLength := newDiscontinuity
      overlapLength := newOverlap
      frameCount := acc.frameCount + 1
      invalidDTS := acc.invalidDTS }

/--
  Fold over the "remaining items" (all but the last) in reverse order.
  In Go, `slices.Backward(dtsAndDurations[:len-1])` iterates from index len-2 down to 0.
  The Go list is in insertion order (oldest first, newest last).
  `Backward(list[:len-1])` yields items from index len-2 down to 0,
  i.e., from second-newest to oldest.
  We model this as `List.foldl processItem acc items.reverse`.
-/
def scanBackward (acc : LoopAcc) (items : List DTSAndDuration) : LoopAcc :=
  items.reverse.foldl processItem acc

/--
  Compute stream quality from a list of DTS/Duration pairs.
  Mirrors `getStreamQualityLocked` at lines 62-136 of stream_measurements.go.

  The list is in chronological order (oldest first, newest last).
-/
def getStreamQuality (entries : List DTSAndDuration) : StreamQuality :=
  match entries with
  | [] =>
    -- Empty input: all metrics = 0
    { continuityNum := 0, continuityDen := 1
      overlapNum := 0, overlapDen := 1
      frameRateNum := 0, frameRateDen := 1
      invalidDTS := 0 }
  | h :: t =>
    let last := (h :: t).getLast (by simp)
    let endTSInt := last.dts + last.duration
    let initAcc : LoopAcc :=
      { prevDTS := last.dts
        minDTS := endTSInt
        discontinuityLength := 0
        overlapLength := 0
        frameCount := 1
        invalidDTS := 0 }
    -- Process all but the last entry, in reverse
    let items := (h :: t).dropLast
    let finalAcc := scanBackward initAcc items
    let intervalInt := endTSInt - finalAcc.minDTS
    if intervalInt > 0 then
      -- continuity = 1 - discontinuity/interval = (interval - discontinuity) / interval
      { continuityNum := intervalInt - finalAcc.discontinuityLength
        continuityDen := intervalInt
        overlapNum := finalAcc.overlapLength
        overlapDen := intervalInt
        frameRateNum := finalAcc.frameCount
        frameRateDen := intervalInt
        invalidDTS := finalAcc.invalidDTS }
    else
      -- interval ≤ 0: all ratios are 0
      { continuityNum := 0, continuityDen := 1
        overlapNum := 0, overlapDen := 1
        frameRateNum := 0, frameRateDen := 1
        invalidDTS := finalAcc.invalidDTS }

/-- A list of entries is "contiguous" if each item's DTS + Duration = the next item's DTS. -/
def isContiguous : List DTSAndDuration → Bool
  | [] => true
  | [_] => true
  | a :: b :: rest => (a.dts + a.duration == b.dts) && isContiguous (b :: rest)

/-- A list has strictly increasing DTS values. -/
def strictlyIncreasingDTS : List DTSAndDuration → Bool
  | [] => true
  | [_] => true
  | a :: b :: rest => (a.dts < b.dts) && strictlyIncreasingDTS (b :: rest)

end Quality
