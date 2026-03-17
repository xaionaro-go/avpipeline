-- DiffTest/ReduceFramerate.lean: Differential test driver for Spec.ReduceFramerate
--
-- Protocol (line-based, space-separated):
--   Input:  <num> <den> <frameID>
--   Output: 0 or 1

import Spec.ReduceFramerate

namespace DiffTest.ReduceFramerate

private def processLine (line : String) : Except String String := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  match parts with
  | [numStr, denStr, fidStr] =>
    let num ← numStr.toNat?.elim (throw "invalid num") pure
    let den ← denStr.toNat?.elim (throw "invalid den") pure
    let fid ← fidStr.toNat?.elim (throw "invalid frameID") pure
    let pass := ReduceState.shouldPass num den fid
    pure (if pass then "1" else "0")
  | _ => throw "expected: <num> <den> <frameID>"

def run : IO Unit := do
  let stdin ← IO.getStdin
  let mut line ← stdin.getLine
  while !line.isEmpty do
    let trimmed := line.trim
    if !trimmed.isEmpty then
      match processLine trimmed with
      | .ok result => IO.println result
      | .error msg => IO.eprintln s!"error: {msg}"
    line ← stdin.getLine

end DiffTest.ReduceFramerate
