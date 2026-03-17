-- DiffTest/Rational.lean: Differential test harness for Spec.Rational
--
-- Protocol (line-based, space-separated):
--   Input:  op a_num a_den [b_num b_den]
--     op ∈ {"mul", "div", "reverse"}
--     For "reverse", b_num and b_den are omitted.
--   Output: num den

import Spec.Rational

namespace DiffTest.Rational

private def parseInt (s : String) : Except String Int :=
  if s.startsWith "-" then
    match s.drop 1 |>.toNat? with
    | some n => pure (-↑n)
    | none   => throw s!"invalid int: {s}"
  else
    match s.toNat? with
    | some n => pure ↑n
    | none   => throw s!"invalid int: {s}"

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "reverse" =>
      match args with
      | [nStr, dStr] =>
        let n ← parseInt nStr
        let d ← parseInt dStr
        let r : Rational := ⟨n, d⟩
        let result := r.reverse
        pure s!"{result.num} {result.den}"
      | _ => throw "reverse: expected 2 args (num den)"
    | "mul" =>
      match args with
      | [anStr, adStr, bnStr, bdStr] =>
        let an ← parseInt anStr
        let ad ← parseInt adStr
        let bn ← parseInt bnStr
        let bd ← parseInt bdStr
        let a : Rational := ⟨an, ad⟩
        let b : Rational := ⟨bn, bd⟩
        let result := a.mul b
        pure s!"{result.num} {result.den}"
      | _ => throw "mul: expected 4 args (a_num a_den b_num b_den)"
    | "div" =>
      match args with
      | [anStr, adStr, bnStr, bdStr] =>
        let an ← parseInt anStr
        let ad ← parseInt adStr
        let bn ← parseInt bnStr
        let bd ← parseInt bdStr
        let a : Rational := ⟨an, ad⟩
        let b : Rational := ⟨bn, bd⟩
        let result := a.div b
        pure s!"{result.num} {result.den}"
      | _ => throw "div: expected 4 args (a_num a_den b_num b_den)"
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

end DiffTest.Rational
