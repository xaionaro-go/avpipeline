-- Spec/Graph.lean: Formal specification of graph traversal from traverse.go, find.go, next_layer.go
--
-- Models:
--   traverse.go  — DFS with visited set for cycle detection
--   next_layer.go — get push-to targets with deduplication
--   find.go       — find node by ID using traverse

/-- A graph as an adjacency list: each node ID maps to its neighbor IDs. -/
def Graph := List (Nat × List Nat)

namespace Graph

/-- Look up neighbors of a node in the adjacency list. -/
def neighbors (g : Graph) (n : Nat) : List Nat :=
  match g with
  | [] => []
  | (id, ns) :: rest =>
    if id == n then ns
    else neighbors rest n

/--
  Mirrors `nextLayer()` from next_layer.go: collects push-to targets for a list
  of nodes and deduplicates them. Uses an accumulator set (as list) for dedup.
-/
def nextLayerAux (g : Graph) (nodes : List Nat) (seen : List Nat) : List Nat :=
  match nodes with
  | [] => []
  | n :: rest =>
    let ns := g.neighbors n
    let new_ := ns.filter (fun x => !seen.contains x)
    let deduped := new_.foldl (fun (acc : List Nat) x =>
      if acc.contains x then acc else acc ++ [x]) []
    deduped ++ nextLayerAux g rest (seen ++ deduped)

/-- Simplified nextLayer for a single node: deduplicate its neighbors. -/
def nextLayer (g : Graph) (n : Nat) : List Nat :=
  (g.neighbors n).foldl (fun (acc : List Nat) x =>
    if acc.contains x then acc else acc ++ [x]) []

/--
  Core DFS traversal mirroring `traverse()` from traverse.go.
  Returns the final visited set after traversal.
  For each node: skip if visited, mark visited, recurse into nextLayer children.
  Uses a fuel parameter for termination.
-/
def dfs (g : Graph) (fuel : Nat) (visited : List Nat) (roots : List Nat) : List Nat :=
  match fuel with
  | 0 => visited
  | fuel' + 1 =>
    match roots with
    | [] => visited
    | n :: rest =>
      if visited.contains n then
        dfs g fuel' visited rest
      else
        let visited' := visited ++ [n]
        let children := g.nextLayer n
        let visited'' := dfs g fuel' visited' children
        dfs g fuel' visited'' rest

/--
  Traverse entry point: DFS from a list of roots with empty visited set.
-/
def traverse (g : Graph) (fuel : Nat) (roots : List Nat) : List Nat :=
  dfs g fuel [] roots

/--
  Mirrors `FindNodeByObjectID()` from find.go: searches for a target node
  in the reachable set. Returns true iff target is in the DFS traversal result.
-/
def find (g : Graph) (fuel : Nat) (roots : List Nat) (target : Nat) : Bool :=
  (traverse g fuel roots).contains target

/-- A node is reachable from roots if it appears in a sufficiently-fueled traversal. -/
def reachable (g : Graph) (roots : List Nat) (target : Nat) : Prop :=
  ∃ fuel, (traverse g fuel roots).contains target = true

end Graph
