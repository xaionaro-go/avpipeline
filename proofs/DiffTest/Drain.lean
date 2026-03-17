-- DiffTest/Drain.lean: Differential test harness for Spec.Drain

import Spec.Drain

namespace DiffTest.Drain

private def parseBool (s : String) : Option Bool :=
  if s == "1" then some true
  else if s == "0" then some false
  else none

private def boolToStr (b : Bool) : String :=
  if b then "1" else "0"

/-- Parse triples of 0/1 into NodeState list. -/
private def parseNodes (args : List String) : Option (List NodeState) :=
  go args []
where
  go : List String → List NodeState → Option (List NodeState)
  | [], acc => some acc.reverse
  | f :: d :: b :: rest, acc => do
    let fv ← parseBool f
    let dv ← parseBool d
    let bv ← parseBool b
    go rest ({ flushed := fv, drained := dv, blocked := bv } :: acc)
  | _, _ => none

private def nodeToStr (n : NodeState) : String :=
  s!"{boolToStr n.flushed} {boolToStr n.drained} {boolToStr n.blocked}"

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "isDrained" =>
      let nodes ← (parseNodes args).elim (throw "isDrained: invalid node triples") pure
      pure (boolToStr (Drain.isDrained nodes))
    | "drain" =>
      match args with
      | [] => throw "drain: missing setBlock"
      | sbStr :: rest =>
        let setBlock : Option Bool :=
          if sbStr == "1" then some true
          else if sbStr == "0" then some false
          else none  -- "none" or any non-0/1 means no setBlock
        let nodes ← (parseNodes rest).elim (throw "drain: invalid node triples") pure
        let result := Drain.drain setBlock nodes
        let parts := result.map nodeToStr
        pure (String.intercalate " " parts)
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

end DiffTest.Drain
