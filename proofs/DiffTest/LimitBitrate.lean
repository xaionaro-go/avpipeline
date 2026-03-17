-- DiffTest/LimitBitrate.lean: Differential test driver for LimitBitrate spec.
-- Reads test vectors from stdin, applies spec, writes results to stdout.

import Spec.LimitBitrate

namespace DiffTest.LimitBitrate

/-- Parse the config line: <averageBitRate> <averagingBufferBits> -/
def parseConfig (line : String) : Option LimitBitrateConfig := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  guard (parts.length ≥ 2)
  let avgBitRate ← parts[0]!.toNat?
  let avgBufferBits ← parts[1]!.toNat?
  pure ⟨avgBitRate, avgBufferBits⟩

/-- Parse an input line: <inputKind:0|1|2> <packetSize> <isKeyframe:0|1> <drainAmount> -/
def parseInput (line : String) : Option (InputKind × Nat) := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  guard (parts.length ≥ 4)
  let kindN ← parts[0]!.toNat?
  let packetSize ← parts[1]!.toNat?
  let isKeyframeN ← parts[2]!.toNat?
  let drainAmount ← parts[3]!.toNat?
  let kind := match kindN with
    | 0 => InputKind.nonVideo
    | 1 => InputKind.videoFrame
    | _ => InputKind.videoPacket packetSize (isKeyframeN != 0)
  pure (kind, drainAmount)

/-- Format output for one input. -/
def formatOutput (accepted : Bool) (consumedBits : Nat) : String :=
  let acc := if accepted then "1" else "0"
  s!"{acc} {consumedBits}"

def main : IO Unit := do
  let stdin ← IO.getStdin
  -- Read config line
  let configLine ← stdin.getLine
  if configLine.isEmpty then
    IO.eprintln "ERROR: expected config line"
    return
  let some cfg := parseConfig configLine.trimRight
    | do IO.eprintln s!"ERROR: bad config: {configLine.trimRight}"; return
  -- Read input lines
  let mut state := LimitBitrateState.init
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
        match parseInput trimmed with
        | none => IO.println s!"ERROR: bad input: {trimmed}"
        | some (kind, drainAmount) =>
          let (accepted, newState) := state.step cfg kind drainAmount
          let out := formatOutput accepted newState.consumed
          IO.println out
          state := newState

end DiffTest.LimitBitrate
