-- DiffTest/ReorderDTS.lean: Differential test harness for Spec.ReorderDTS
--
-- Protocol (line-based, space-separated):
--   Input line 1: <maxDTSDiff> <capacity> <discardMode:0|1> <numStreams>
--   Subsequent lines: <dts> <streamKey>
--   Per input line output: <num_emitted> <emitted_dts_1> <emitted_dts_2> ...
--   Final line after all inputs: flush <emitted_dts_1> <emitted_dts_2> ...

import Spec.ReorderDTS

namespace DiffTest.ReorderDTS

private def parseInt (s : String) : Int :=
  if s.startsWith "-" then
    match s.drop 1 |>.toNat? with
    | some n => -↑n
    | none   => 0
  else
    match s.toNat? with
    | some n => ↑n
    | none   => 0

/-- Format a list of DTS values as space-separated string. -/
private def formatDTSList (xs : List Int) : String :=
  " ".intercalate (xs.map toString)

/-- Flush all remaining items from the global queue. -/
private def flushAll (s : ReorderState) (fuel : Nat) : ReorderState :=
  match fuel with
  | 0 => s
  | fuel' + 1 =>
    match s.sendOneItemFromQueue with
    | none => s
    | some (_, s') => flushAll s' fuel'

def run : IO Unit := do
  let stdin ← IO.getStdin
  -- Read header
  let headerLine ← stdin.getLine
  if headerLine.isEmpty then return
  let headerParts := headerLine.trim.splitOn " " |>.filter (· ≠ "")
  match headerParts with
  | [maxDiffStr, capStr, discardStr, _numStreamsStr] =>
    let maxDiff := maxDiffStr.toNat?.getD 0
    let cap := capStr.toNat?.getD 0
    let discard := discardStr == "1"
    let mut state := ReorderState.init maxDiff cap discard
    -- Process input lines
    let mut line ← stdin.getLine
    while !line.isEmpty do
      let trimmed := line.trim
      if !trimmed.isEmpty then
        let parts := trimmed.splitOn " " |>.filter (· ≠ "")
        match parts with
        | [dtsStr, skStr] =>
          let dts := parseInt dtsStr
          let sk := skStr.toNat?.getD 0
          let item : Item := { dts := dts, streamKey := sk }
          let emittedBefore := state.emitted
          state := state.pushToQueue item
          -- Compute newly emitted items (emitted is most-recent-first)
          let newEmitted := state.emitted.take (state.emitted.length - emittedBefore.length)
          -- newEmitted is most-recent-first; reverse for chronological output
          let newEmittedChrono := newEmitted.reverse
          let countStr := toString newEmittedChrono.length
          if newEmittedChrono.isEmpty then
            IO.println countStr
          else
            IO.println s!"{countStr} {formatDTSList newEmittedChrono}"
        | _ => pure ()
      line ← stdin.getLine
    -- Flush remaining items
    let emittedBefore := state.emitted
    state := flushAll state state.globalQueue.length
    let newEmitted := state.emitted.take (state.emitted.length - emittedBefore.length)
    let newEmittedChrono := newEmitted.reverse
    if newEmittedChrono.isEmpty then
      IO.println "flush"
    else
      IO.println s!"flush {formatDTSList newEmittedChrono}"
  | _ =>
    IO.eprintln "error: expected header: maxDTSDiff capacity discardMode numStreams"

end DiffTest.ReorderDTS
