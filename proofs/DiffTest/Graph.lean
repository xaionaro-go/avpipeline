-- DiffTest/Graph.lean: Differential test harness for Spec.Graph

import Spec.Graph

namespace DiffTest.Graph

private def parseNat (s : String) : Except String Nat :=
  match s.toNat? with
  | some n => pure n
  | none => throw s!"invalid nat: {s}"

private def parseNats (ss : List String) : Except String (List Nat) :=
  ss.mapM parseNat

private def splitOnPipe (tokens : List String) : List String × List String :=
  go tokens []
where
  go : List String → List String → List String × List String
  | [], acc => (acc.reverse, [])
  | "|" :: rest, acc => (acc.reverse, rest)
  | t :: rest, acc => go rest (t :: acc)

private def takeN (n : Nat) (tokens : List String) (acc : List Nat) :
    Except String (List Nat × List String) :=
  match n with
  | 0 => pure (acc.reverse, tokens)
  | k + 1 =>
    match tokens with
    | [] => throw "graph: not enough neighbors"
    | s :: rest => do
      let v ← parseNat s
      takeN k rest (v :: acc)

private def parseGraphNodes (remaining : Nat) (tokens : List String) (acc : Graph) :
    Except String Graph :=
  match remaining with
  | 0 => pure acc.reverse
  | r + 1 =>
    match tokens with
    | [] => throw "graph: expected more nodes"
    | idStr :: rest =>
      match rest with
      | [] => throw "graph: missing num_neighbors"
      | numNStr :: rest' => do
        let nodeId ← parseNat idStr
        let numN ← parseNat numNStr
        let (neighbors, remainingTokens) ← takeN numN rest' []
        parseGraphNodes r remainingTokens ((nodeId, neighbors) :: acc)

private def parseGraph (tokens : List String) : Except String Graph := do
  match tokens with
  | [] => throw "graph: missing num_nodes"
  | numNodesStr :: rest =>
    let numNodes ← parseNat numNodesStr
    parseGraphNodes numNodes rest []

/--
  Parse traverse command arguments.
  Format: `<num_nodes> <node1_id> <num_neighbors> <n1> <n2> ... | <root1> <root2> ...`
-/
private def parseTraverse (args : List String) : Except String (Graph × List Nat) := do
  let (graphTokens, rootTokens) := splitOnPipe args
  let graph ← parseGraph graphTokens
  let roots ← parseNats rootTokens
  pure (graph, roots)

/-- Process a single test line. -/
def processLine (line : String) : Except String String := do
  let tokens := line.splitOn " " |>.filter (· ≠ "")
  match tokens with
  | [] => throw "empty line"
  | cmd :: args =>
    match cmd with
    | "traverse" => do
      let (graph, roots) ← parseTraverse args
      -- Fuel: total nodes * 2 + 10 for safety
      let fuel := graph.length * 2 + 10
      let visited := Graph.traverse graph fuel roots
      let parts := visited.map toString
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

end DiffTest.Graph
