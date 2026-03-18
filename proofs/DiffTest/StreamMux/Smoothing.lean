-- DiffTest/StreamMux/Smoothing.lean: Differential test harness for
-- Spec.StreamMux.Smoothing.
--
-- Protocol (line-based, space-separated):
--   Input:  smoothing <old> <new> <inertiaNum> <inertiaDen> <count>
--   Output: lean_smoothing <num> <den> <result>
--     where result = num / den (integer division)
--
--   Input:  packfps <num> <den>
--   Output: lean_packfps <packed> <unpacked_num> <unpacked_den>

import Spec.StreamMux.Smoothing

open StreamMux

namespace DiffTest.StreamMux.Smoothing

private def parseInt (s : String) : Except String Nat :=
  match s.toNat? with
  | some n => pure n
  | none   => throw s!"invalid nat: {s}"

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "smoothing" =>
      match args with
      | [oldS, newS, inumS, idenS, countS] =>
        let old ← parseInt oldS
        let new_ ← parseInt newS
        let inum ← parseInt inumS
        let iden ← parseInt idenS
        let count ← parseInt countS

        let num := smoothedNum old new_ inum iden count
        let den := smoothedDen iden count
        let result := if den = 0 then 0 else num / den

        pure s!"lean_smoothing {num} {den} {result}"
      | _ => throw "smoothing: expected 5 arguments"
    | "packfps" =>
      match args with
      | [numS, denS] =>
        let num ← parseInt numS
        let den ← parseInt denS

        let packed := packFPS num den
        let unum := unpackFPSNum packed
        let uden := unpackFPSDen packed

        pure s!"lean_packfps {packed} {unum} {uden}"
      | _ => throw "packfps: expected 2 arguments"
    | _ => throw s!"unknown command: {cmd}"

def run : IO Unit := do
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

end DiffTest.StreamMux.Smoothing
