-- DiffTest/Pipeline/Barrier.lean: Differential test harness for Pipeline.outputSwitchGetState

import Spec.Pipeline.Barrier

namespace DiffTest.Pipeline.Barrier

open _root_.Pipeline

private def parseMediaType (s : String) : Option PipeMediaType :=
  match s with
  | "video"    => some .video
  | "audio"    => some .audio
  | "subtitle" => some .subtitle
  | "data"     => some .data
  | _          => none

private def parseBool (s : String) : Option Bool :=
  match s with
  | "true"  => some true
  | "false" => some false
  | _       => none

private def parseKeepUnless (s : String) : Option FilterCondition :=
  match s with
  | "keepunless"         => some (standardKeepUnless false)
  | "keepunless_corrupt" => some (standardKeepUnless true)
  | "always"             => some .always
  | "never"              => some .never
  | _                    => none

private def decisionToStr : BarrierDecision → String
  | .pass  => "pass"
  | .drop  => "drop"
  | .block => "block"

/-- Process a single test line. Returns the result string or an error. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "switch" =>
      match args with
      | [curStr, nextStr, outStr, mtStr, kfStr, kuStr] =>
        let cur ← match curStr.toNat? with
          | some n => pure n
          | none   => throw s!"invalid currentValue: {curStr}"
        let next ← match nextStr with
          | "none" => pure none
          | _      => match nextStr.toNat? with
            | some n => pure (some n)
            | none   => throw s!"invalid nextValue: {nextStr}"
        let outID ← match outStr.toNat? with
          | some n => pure n
          | none   => throw s!"invalid outputID: {outStr}"
        let mt ← (parseMediaType mtStr).elim (throw s!"invalid media type: {mtStr}") pure
        let kf ← (parseBool kfStr).elim (throw s!"invalid keyframe flag: {kfStr}") pure
        let ku ← (parseKeepUnless kuStr).elim (throw s!"unknown keepUnless: {kuStr}") pure
        let sw : SwitchState := {
          currentValue  := cur
          nextValue     := next
          previousValue := 0
          keepUnless    := ku
        }
        let pkt : PipePacket := {
          mediaType   := mt
          isKeyFrame  := kf
          pts         := 0
          dts         := 0
          streamIndex := 0
          size        := 0
        }
        let (_, decision) := outputSwitchGetState sw outID pkt
        pure (decisionToStr decision)
      | _ => throw "switch: expected 6 arguments: <currentValue> <nextValue_or_none> <outputID> <mediaType> <isKeyFrame> <keepUnlessName>"
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

end DiffTest.Pipeline.Barrier
