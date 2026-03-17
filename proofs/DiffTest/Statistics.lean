-- DiffTest/Statistics.lean: Differential test harness for Spec.Statistics
--
-- Protocol (line-based, space-separated):
--   Input:  n media_type_1 msg_size_1 media_type_2 msg_size_2 ...
--     n = number of increments to apply
--     media_type ∈ {"video", "audio", "other", "unknown"}
--     msg_size = non-negative integer
--   Output: video_count video_bytes audio_count audio_bytes
--           other_count other_bytes unknown_count unknown_bytes
--           total_count total_bytes

import Spec.Statistics

namespace DiffTest.Statistics

private def parseNat (s : String) : Except String Nat :=
  match s.toNat? with
  | some n => pure n
  | none   => throw s!"invalid nat: {s}"

private def parseMediaType (s : String) : Except String MediaType :=
  match s with
  | "video"   => pure .video
  | "audio"   => pure .audio
  | "other"   => pure .other
  | "unknown" => pure .unknown
  | _         => throw s!"invalid media_type: {s}"

private def parseIncrements (n : Nat) (tokens : List String) :
    Except String (List (MediaType × Nat)) := do
  let rec go (remaining : Nat) (ts : List String) (acc : List (MediaType × Nat)) :
      Except String (List (MediaType × Nat)) :=
    match remaining with
    | 0 => pure acc.reverse
    | k + 1 =>
      match ts with
      | mtStr :: szStr :: rest => do
        let mt ← parseMediaType mtStr
        let sz ← parseNat szStr
        go k rest ((mt, sz) :: acc)
      | _ => throw s!"expected {remaining} more (media_type, msg_size) pairs"
  go n tokens []

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | nStr :: rest =>
    let n ← parseNat nStr
    let increments ← parseIncrements n rest
    let css := increments.foldl
      (fun s (mt, sz) => s.increment mt sz)
      CountersSubSection.zero
    let tc := css.totalCount
    let tb := css.totalBytes
    pure s!"{css.video.count} {css.video.bytes} {css.audio.count} {css.audio.bytes} {css.other.count} {css.other.bytes} {css.unknown.count} {css.unknown.bytes} {tc} {tb}"

def main : IO Unit := do
  let stdin ← IO.getStdin
  let mut done := false
  while !done do
    let line ← stdin.getLine
    if line.isEmpty then
      done := true
    else
      let line := line.trimRight
      if line.isEmpty then continue
      match processLine line with
      | .ok result => IO.println result
      | .error msg => IO.eprintln s!"error: {msg}"

end DiffTest.Statistics
