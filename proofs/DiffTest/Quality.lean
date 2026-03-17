-- DiffTest/Quality.lean: Differential test harness for Spec.Quality
--
-- Protocol (line-based, space-separated):
--   Input line 1: <num_entries>
--   Subsequent lines: <dts> <duration>
--   Output: <continuity_num> <continuity_den> <overlap_num> <overlap_den>
--           <frameRate_num> <frameRate_den> <invalidDTS>

import Spec.Quality

namespace DiffTest.Quality

private def parseInt (s : String) : Int :=
  if s.startsWith "-" then
    match s.drop 1 |>.toNat? with
    | some n => -↑n
    | none   => 0
  else
    match s.toNat? with
    | some n => ↑n
    | none   => 0

private def readEntry (stdin : IO.FS.Stream) : IO (Option DTSAndDuration) := do
  let line ← stdin.getLine
  if line.isEmpty then return none
  let parts := line.trim.splitOn " " |>.filter (· ≠ "")
  match parts with
  | [dtsStr, durStr] =>
    return some { dts := parseInt dtsStr, duration := parseInt durStr }
  | _ => return none

def run : IO Unit := do
  let stdin ← IO.getStdin
  -- Read number of entries
  let headerLine ← stdin.getLine
  if headerLine.isEmpty then return
  let numEntries := headerLine.trim.toNat?.getD 0
  -- Read entries
  let mut entries : List DTSAndDuration := []
  for _ in List.range numEntries do
    match ← readEntry stdin with
    | some e => entries := entries ++ [e]
    | none   => break
  -- Compute quality
  let q := Quality.getStreamQuality entries
  IO.println s!"{q.continuityNum} {q.continuityDen} {q.overlapNum} {q.overlapDen} {q.frameRateNum} {q.frameRateDen} {q.invalidDTS}"

end DiffTest.Quality
