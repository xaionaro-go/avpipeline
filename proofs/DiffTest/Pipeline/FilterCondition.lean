-- DiffTest/Pipeline/FilterCondition.lean: Differential test harness for Pipeline.FilterCondition

import Spec.Pipeline.FilterCondition

namespace DiffTest.Pipeline.FilterCondition

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

private def parseCondition (s : String) : Option _root_.Pipeline.FilterCondition :=
  match s with
  | "keepunless"         => some (_root_.Pipeline.standardKeepUnless false)
  | "keepunless_corrupt" => some (_root_.Pipeline.standardKeepUnless true)
  | "audio_sub_data"     => some _root_.Pipeline.audioSubtitleDataCond
  | "video"              => some _root_.Pipeline.videoCond
  | "always"             => some .always
  | "never"              => some .never
  | _                    => none

private def boolToStr (b : Bool) : String :=
  if b then "1" else "0"

/-- Process a single test line. Returns the result string or an error. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "match" =>
      match args with
      | [mtStr, kfStr, condStr] =>
        let mt ← (parseMediaType mtStr).elim (throw s!"invalid media type: {mtStr}") pure
        let kf ← (parseBool kfStr).elim (throw s!"invalid keyframe flag: {kfStr}") pure
        let cond ← (parseCondition condStr).elim (throw s!"unknown condition: {condStr}") pure
        let pkt : PipePacket := {
          mediaType := mt
          isKeyFrame := kf
          pts := 0
          dts := 0
          streamIndex := 0
          size := 0
        }
        pure (boolToStr (cond.match pkt))
      | _ => throw "match: expected 3 arguments: <mediaType> <isKeyFrame> <conditionName>"
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

end DiffTest.Pipeline.FilterCondition
