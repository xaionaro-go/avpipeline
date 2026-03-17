-- DiffTest/LimitFramerate.lean: Differential test driver for LimitFramerate spec.
-- Reads test vectors from stdin, applies spec, writes results to stdout.

import Spec.LimitFramerate

namespace DiffTest.LimitFramerate

/-- Parse the config line: <maxFPS_num> <maxFPS_den> <minDur_num> <minDur_den> -/
def parseConfig (line : String) : Option (Int × Rational) := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  guard (parts.length ≥ 4)
  let maxFPSNum ← parts[0]!.toInt?
  let _maxFPSDen ← parts[1]!.toInt?
  let minDurNum ← parts[2]!.toInt?
  let minDurDen ← parts[3]!.toInt?
  pure (maxFPSNum, ⟨minDurNum, minDurDen⟩)

/-- Parse a frame line: <pts> <dur> -/
def parseFrame (line : String) : Option LimitState.Input := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  guard (parts.length ≥ 2)
  let pts ← parts[0]!.toInt?
  let dur ← parts[1]!.toInt?
  pure ⟨pts, dur⟩

/-- Format output for one frame. -/
def formatOutput (accepted : Bool) (newMinPTS : Int) (newDebt : Int) : String :=
  let acc := if accepted then "1" else "0"
  s!"{acc} {newMinPTS} {newDebt}"

def main : IO Unit := do
  let stdin ← IO.getStdin
  -- Read config line
  let configLine ← stdin.getLine
  if configLine.isEmpty then
    IO.eprintln "ERROR: expected config line"
    return
  let some (maxFPSNum, minDuration) := parseConfig configLine.trimRight
    | do IO.eprintln s!"ERROR: bad config: {configLine.trimRight}"; return
  -- Read frame lines
  let mut state := LimitState.init
  let mut done := false
  while !done do
    let line ← stdin.getLine
    if line.isEmpty then
      done := true
    else
      let trimmed := line.trimRight
      if trimmed.isEmpty then
        pure ()
      else
        match parseFrame trimmed with
        | none => IO.println s!"ERROR: bad input: {trimmed}"
        | some inp =>
          let result := state.match' inp minDuration maxFPSNum
          let out := formatOutput result.accepted result.newState.minPTS result.newState.debt
          IO.println out
          state := result.newState

end DiffTest.LimitFramerate
