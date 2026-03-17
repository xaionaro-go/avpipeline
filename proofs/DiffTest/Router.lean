-- DiffTest/Router.lean: Differential test driver for Spec.Router and Spec.RouterExtended
--
-- Protocol (line-based, space-separated):
--   Input: a sequence of operations, one per line:
--     add <publisher_id> <mode:0-4>
--     remove <publisher_id>
--     open
--     close
--   Mode: 0=undefined, 1=exclusiveTakeover, 2=exclusiveFail, 3=sharedTakeover, 4=sharedFail
--
--   State starts as: open, empty publishers.
--   Output per operation: <result_tag> <publisher_count>

import Spec.RouterExtended

namespace DiffTest.Router

private def modeOfNat : Nat → Option PublishMode
  | 0 => some .undefined
  | 1 => some .exclusiveTakeover
  | 2 => some .exclusiveFail
  | 3 => some .sharedTakeover
  | 4 => some .sharedFail
  | _ => none

private def processOp (state : RouteState) (tokens : List String) :
    Except String (String × RouteState) := do
  match tokens with
  | ["add", pidStr, modeStr] =>
    let pid ← pidStr.toNat?.elim (throw "add: invalid publisher_id") pure
    let modeN ← modeStr.toNat?.elim (throw "add: invalid mode") pure
    let mode ← (modeOfNat modeN).elim (throw "add: unknown mode number") pure
    let pub : Publisher := ⟨pid, mode⟩
    let (result, newState) := state.addPublisher pub
    let tag := match result with
      | .ok _                 => "ok"
      | .errRouteClosed       => "errRouteClosed"
      | .errAlreadyAPublisher => "errAlreadyAPublisher"
      | .errAlreadyHasPublisher => "errAlreadyHasPublisher"
      | .errUnknownMode       => "errUnknownMode"
    pure (s!"{tag} {newState.publishers.length}", newState)
  | ["remove", pidStr] =>
    let pid ← pidStr.toNat?.elim (throw "remove: invalid publisher_id") pure
    -- Try removing with each possible mode; match by id only.
    -- The spec matches by (id, mode) equality. We need to find the publisher in the list.
    let found := state.publishers.find? (fun p => p.id == pid)
    match found with
    | some pub =>
      let (result, newState) := state.removePublisher pub
      let tag := match result with
        | .ok _                  => "ok"
        | .errPublisherNotFound  => "errPublisherNotFound"
      pure (s!"{tag} {newState.publishers.length}", newState)
    | none =>
      -- Publisher not in list; manufacture a dummy to get errPublisherNotFound.
      let dummy : Publisher := ⟨pid, .undefined⟩
      let (result, newState) := state.removePublisher dummy
      let tag := match result with
        | .ok _                  => "ok"
        | .errPublisherNotFound  => "errPublisherNotFound"
      pure (s!"{tag} {newState.publishers.length}", newState)
  | ["open"] =>
    let (result, newState) := state.openNode
    let tag := match result with
      | .ok              => "ok"
      | .errAlreadyOpen  => "errAlreadyOpen"
      | .errAlreadyClosed => "errAlreadyClosed"
    pure (s!"{tag} {newState.publishers.length}", newState)
  | ["close"] =>
    let (result, newState) := state.closeNode
    let tag := match result with
      | .ok              => "ok"
      | .errAlreadyOpen  => "errAlreadyOpen"
      | .errAlreadyClosed => "errAlreadyClosed"
    pure (s!"{tag} {newState.publishers.length}", newState)
  | _ => throw s!"unknown operation: {tokens}"

def run : IO Unit := do
  let stdin ← IO.getStdin
  let mut state : RouteState := ⟨true, []⟩
  let mut line ← stdin.getLine
  while !line.isEmpty do
    let trimmed := line.trim
    if !trimmed.isEmpty then
      let tokens := trimmed.splitOn " " |>.filter (· ≠ "")
      match processOp state tokens with
      | .ok (result, newState) =>
        IO.println result
        state := newState
      | .error msg => IO.eprintln s!"error: {msg}"
    line ← stdin.getLine

end DiffTest.Router
