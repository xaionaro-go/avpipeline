-- DiffTest/Condition.lean: Differential test harness for Spec.Condition

import Spec.Condition

namespace DiffTest.Condition

private def parseBool (s : String) : Option Bool :=
  if s == "1" then some true
  else if s == "0" then some false
  else none

private def parseBools (ss : List String) : Option (List Bool) :=
  ss.mapM parseBool

private def boolToStr (b : Bool) : String :=
  if b then "1" else "0"

/-- Process a single test line. Returns the result string or an error. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "and" =>
      let bools ← (parseBools args).elim (throw "invalid bool") pure
      -- Build list of constant conditions and evaluate andMatch
      let conds : List (Condition Unit) := bools.map fun b => fun _ => b
      pure (boolToStr (Condition.andMatch conds ()))
    | "or" =>
      let bools ← (parseBools args).elim (throw "invalid bool") pure
      let conds : List (Condition Unit) := bools.map fun b => fun _ => b
      pure (boolToStr (Condition.orMatch conds ()))
    | "not_and" =>
      let bools ← (parseBools args).elim (throw "invalid bool") pure
      let conds : List (Condition Unit) := bools.map fun b => fun _ => b
      pure (boolToStr (Condition.notMatch conds ()))
    | "in" =>
      match args with
      | [] => throw "in: missing target"
      | target :: elems =>
        let t ← (target.toNat?).elim (throw "in: invalid target") pure
        let es ← (elems.mapM (fun s => s.toNat?.elim (throw "in: invalid elem") pure))
        pure (boolToStr (Condition.inSet es t))
    | "static" =>
      match args with
      | [b] =>
        let bv ← (parseBool b).elim (throw "static: invalid bool") pure
        let c := Condition.static (T := Unit) bv
        pure (boolToStr (c ()))
      | _ => throw "static: expected exactly one argument"
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

end DiffTest.Condition
