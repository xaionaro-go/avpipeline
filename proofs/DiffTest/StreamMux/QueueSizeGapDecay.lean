-- DiffTest/StreamMux/QueueSizeGapDecay.lean: Differential test harness for
-- Spec.StreamMux.QueueSizeGapDecay.
--
-- Protocol (line-based, space-separated):
--   Input:  gapdecay <currentBR> <inputBR> <outputBR> <queueDur> <derivative>
--                    <checkInterval> <optimal> <minBytes> <gapDecay>
--                    <inertiaInc> <inertiaDec>
--   Output:
--     lean_intermediates <queueDurOptimal> <gap> <gapB> <desiredDeriv>
--                        <derivGap> <bitRateDiff> <rawNewBR> <finalBR>
--     lean_result <finalBR> <isCritical>

import Spec.StreamMux.QueueSizeGapDecay

namespace DiffTest.StreamMux.QueueSizeGapDecay

open GapDecaySpec

private def parseInt (s : String) : Except String Int :=
  if s.startsWith "-" then
    match (s.drop 1).toNat? with
    | some n => pure (-↑n)
    | none   => throw s!"invalid int: {s}"
  else
    match s.toNat? with
    | some n => pure ↑n
    | none   => throw s!"invalid int: {s}"

/-- Process a single test line. Returns result lines or an error. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "gapdecay" =>
      match args with
      | [curBR, inBR, outBR, qDur, deriv, chkInt, opt, minB, decay, incI, decI] =>
        let currentBR ← parseInt curBR
        let inputBR ← parseInt inBR
        let outputBR ← parseInt outBR
        let queueDur ← parseInt qDur
        let derivative ← parseInt deriv
        let checkInterval ← parseInt chkInt
        let optimal ← parseInt opt
        let minBytes ← parseInt minB
        let gapDecayTime ← parseInt decay
        let inertiaInc ← parseInt incI
        let inertiaDec ← parseInt decI

        let cfg : GapDecayConfig := {
          queueDurationOptimal := optimal
          queueSizeMinBytes := minBytes
          gapDecay := gapDecayTime
          inertiaIncrease := inertiaInc
          inertiaDecrease := inertiaDec
        }

        let req : GapDecayRequest := {
          currentBitRate := currentBR
          inputBitRate := inputBR
          actualOutputBitRate := outputBR
          queueDuration := queueDur
          actualDerivative := derivative
          checkInterval := checkInterval
        }

        -- Step-by-step computation
        let qDurOpt := queueDurationOptimal cfg req.actualOutputBitRate
        let g := gap req.queueDuration qDurOpt
        let gB := gapBytes req.actualOutputBitRate g
        let dd := desiredDerivative gB cfg.gapDecay
        let dg := derivativeGap dd req.actualDerivative
        let brd := bitRateDiff dg
        let rawBR := rawNewBitRate req.currentBitRate brd

        -- Apply inertia
        let finalBR := applyInertia rawBR req.currentBitRate
            req.checkInterval cfg.inertiaIncrease cfg.inertiaDecrease

        -- Critical flag (uses bitRateDiff, not the diff after inertia)
        let crit := isCritical brd finalBR req.actualOutputBitRate req.inputBitRate

        let critStr := if crit then "true" else "false"

        pure s!"lean_intermediates {qDurOpt} {g} {gB} {dd} {dg} {brd} {rawBR} {finalBR}\nlean_result {finalBR} {critStr}"
      | _ => throw "gapdecay: expected 11 arguments"
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

end DiffTest.StreamMux.QueueSizeGapDecay
