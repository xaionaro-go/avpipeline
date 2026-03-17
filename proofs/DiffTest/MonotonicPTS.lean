-- DiffTest/MonotonicPTS.lean: Differential test driver for MonotonicPTS spec.
-- Reads test vectors from stdin, applies spec, writes results to stdout.

import Spec.MonotonicPTS

namespace DiffTest.MonotonicPTS

/-- Parse a space-separated line into FrameInput + shouldCorrect + tolerance.
    Format: <pts> <dts> <streamIndex> <sourceKey> <shouldCorrect:0|1> <tolerance> -/
def parseLine (line : String) : Option (MonotonicPTSState.FrameInput × Bool × Int) := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  guard (parts.length ≥ 6)
  let pts ← parts[0]!.toInt?
  let dts ← parts[1]!.toInt?
  let streamIndex ← parts[2]!.toNat?
  let sourceKey ← parts[3]!.toNat?
  let shouldCorrectN ← parts[4]!.toNat?
  let tolerance ← parts[5]!.toInt?
  -- dtsIsNoPTS modeled by a 7th field if present, default false
  let dtsIsNoPTS := if parts.length ≥ 7 then parts[6]! == "1" else false
  let inp : MonotonicPTSState.FrameInput :=
    { pts := pts
      dts := dts
      streamIndex := streamIndex
      sourceKey := sourceKey
      dtsIsNoPTS := dtsIsNoPTS }
  pure (inp, shouldCorrectN != 0, tolerance)

/-- Format the output for one frame. -/
def formatOutput (accepted : Bool) (adjustedPTS : Int) (latestPTS : Int) : String :=
  let acc := if accepted then "1" else "0"
  s!"{acc} {adjustedPTS} {latestPTS}"

/-- Process all lines against initial state, accumulating output. -/
def processLines (lines : List String) : List String :=
  let rec go (s : MonotonicPTSState) (remaining : List String) (acc : List String) : List String :=
    match remaining with
    | [] => acc.reverse
    | line :: rest =>
      if line.trim.isEmpty then go s rest acc
      else
        match parseLine line with
        | none => go s rest (s!"ERROR: bad input: {line}" :: acc)
        | some (inp, shouldCorrect, tolerance) =>
          -- The state carries shouldCorrect; update it from input each line
          let s' := { s with shouldCorrect := shouldCorrect }
          let result := s'.matchResult inp tolerance
          let nextSt := s'.matchNextState inp tolerance
          let (accepted, adjustedPTS) := match result with
            | MonotonicPTSState.MatchResult.accepted pts => (true, pts)
            | MonotonicPTSState.MatchResult.rejected => (false, 0)
          let out := formatOutput accepted adjustedPTS nextSt.latestPTS
          go nextSt rest (out :: acc)
  go (MonotonicPTSState.init false) lines []

def main : IO Unit := do
  let stdin ← IO.getStdin
  let mut lines : Array String := #[]
  let mut done := false
  while !done do
    let line ← stdin.getLine
    if line.isEmpty then
      done := true
    else
      lines := lines.push line.trimRight
  let results := processLines lines.toList
  for r in results do
    IO.println r

end DiffTest.MonotonicPTS
