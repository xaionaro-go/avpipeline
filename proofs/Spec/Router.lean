-- Spec/Router.lean: Formal specification of AddPublisherLocked from router/route.go

import Spec.PublishMode

/-- A publisher with a mode, abstracting away the Go interface. -/
structure Publisher where
  id : Nat
  mode : PublishMode
  deriving Repr, DecidableEq

/-- Result of AddPublisherLocked. -/
inductive AddResult where
  | ok (publishers : List Publisher)
  | errRouteClosed
  | errAlreadyAPublisher
  | errAlreadyHasPublisher
  | errUnknownMode
  deriving Repr, DecidableEq

/-- Route state relevant to publisher management. -/
structure RouteState where
  isOpen : Bool
  publishers : List Publisher
  deriving Repr

namespace RouteState

/--
  Mirrors the core logic of `AddPublisherLocked` from route.go lines 227-303.
  This is a pure function extracting the state-transition logic.
-/
def addPublisher (s : RouteState) (p : Publisher) : AddResult × RouteState :=
  -- route must be open
  if !s.isOpen then
    (AddResult.errRouteClosed, s)
  -- publisher must not already be in the list
  else if s.publishers.any (· == p) then
    (AddResult.errAlreadyAPublisher, s)
  -- if no existing publishers, just add
  else if s.publishers.isEmpty then
    let newPubs := s.publishers ++ [p]
    (AddResult.ok newPubs, { s with publishers := newPubs })
  else
    -- conflict resolution based on mode
    match p.mode with
    | PublishMode.exclusiveTakeover =>
      -- remove all existing publishers, add new one
      let newPubs := [p]
      (AddResult.ok newPubs, { s with publishers := newPubs })
    | PublishMode.exclusiveFail =>
      -- fail if any publishers exist
      (AddResult.errAlreadyHasPublisher, s)
    | PublishMode.sharedTakeover =>
      -- keep non-exclusive, remove exclusive publishers
      let kept := s.publishers.filter (fun pub => !pub.mode.isExclusive)
      let newPubs := kept ++ [p]
      (AddResult.ok newPubs, { s with publishers := newPubs })
    | PublishMode.sharedFail =>
      -- if any exclusive publisher exists, fail
      if s.publishers.any (fun pub => pub.mode.isExclusive) then
        (AddResult.errAlreadyHasPublisher, s)
      else
        let newPubs := s.publishers ++ [p]
        (AddResult.ok newPubs, { s with publishers := newPubs })
    | PublishMode.undefined =>
      (AddResult.errUnknownMode, s)

end RouteState
