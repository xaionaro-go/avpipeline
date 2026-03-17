-- DiffTest/Units.lean: Differential test harness for Spec.Units
--
-- Protocol (line-based, space-separated):
--   Input:  op val [val2]
--     Single-arg ops: tob, toB, tobps, toBps
--     Two-arg ops: rateTimeToBytes, rateTimeToBits,
--                  bytesToTime, bitsToTime,
--                  bytesToRate, bitsToRate
--   Output: result (single integer)

import Spec.Units

namespace DiffTest.Units

private def parseInt (s : String) : Except String Int :=
  if s.startsWith "-" then
    match s.drop 1 |>.toNat? with
    | some n => pure (-↑n)
    | none   => throw s!"invalid int: {s}"
  else
    match s.toNat? with
    | some n => pure ↑n
    | none   => throw s!"invalid int: {s}"

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "tob" =>
      match args with
      | [vStr] =>
        let v ← parseInt vStr
        pure s!"{(Units.tob ⟨v⟩).val}"
      | _ => throw "tob: expected 1 arg"
    | "toB" =>
      match args with
      | [vStr] =>
        let v ← parseInt vStr
        pure s!"{(Units.toB ⟨v⟩).val}"
      | _ => throw "toB: expected 1 arg"
    | "tobps" =>
      match args with
      | [vStr] =>
        let v ← parseInt vStr
        pure s!"{(Units.tobps ⟨v⟩).val}"
      | _ => throw "tobps: expected 1 arg"
    | "toBps" =>
      match args with
      | [vStr] =>
        let v ← parseInt vStr
        pure s!"{(Units.toBps ⟨v⟩).val}"
      | _ => throw "toBps: expected 1 arg"
    | "rateTimeToBytes" =>
      match args with
      | [rStr, tStr] =>
        let r ← parseInt rStr
        let t ← parseInt tStr
        pure s!"{(Units.rateTimeToBytes ⟨r⟩ ⟨t⟩).val}"
      | _ => throw "rateTimeToBytes: expected 2 args"
    | "rateTimeToBits" =>
      match args with
      | [rStr, tStr] =>
        let r ← parseInt rStr
        let t ← parseInt tStr
        pure s!"{(Units.rateTimeToBits ⟨r⟩ ⟨t⟩).val}"
      | _ => throw "rateTimeToBits: expected 2 args"
    | "bytesToTime" =>
      match args with
      | [vStr, rStr] =>
        let v ← parseInt vStr
        let r ← parseInt rStr
        pure s!"{(Units.bytesToTime ⟨v⟩ ⟨r⟩).val}"
      | _ => throw "bytesToTime: expected 2 args"
    | "bitsToTime" =>
      match args with
      | [vStr, rStr] =>
        let v ← parseInt vStr
        let r ← parseInt rStr
        pure s!"{(Units.bitsToTime ⟨v⟩ ⟨r⟩).val}"
      | _ => throw "bitsToTime: expected 2 args"
    | "bytesToRate" =>
      match args with
      | [vStr, tStr] =>
        let v ← parseInt vStr
        let t ← parseInt tStr
        pure s!"{(Units.bytesToRate ⟨v⟩ ⟨t⟩).val}"
      | _ => throw "bytesToRate: expected 2 args"
    | "bitsToRate" =>
      match args with
      | [vStr, tStr] =>
        let v ← parseInt vStr
        let t ← parseInt tStr
        pure s!"{(Units.bitsToRate ⟨v⟩ ⟨t⟩).val}"
      | _ => throw "bitsToRate: expected 2 args"
    | _ => throw s!"unknown command: {cmd}"

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

end DiffTest.Units
