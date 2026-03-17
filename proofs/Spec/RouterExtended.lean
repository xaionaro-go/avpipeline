-- Spec/RouterExtended.lean: Extended router specifications
-- Models RemovePublisher, open/close lifecycle, GetRoute modes, and route map operations.

import Spec.Router

open RouteState

/-! ## RemovePublisher -/

/-- Result of RemovePublisherLocked. -/
inductive RemoveResult where
  | ok (publishers : List Publisher)
  | errPublisherNotFound
  deriving Repr, DecidableEq

/--
  Mirrors `RemovePublisherLocked` from route.go lines 315-340.
  Searches publishers list for the target; if found, removes it and returns
  the updated list. Otherwise returns errPublisherNotFound.
-/
def RouteState.removePublisher (s : RouteState) (p : Publisher) : RemoveResult × RouteState :=
  if s.publishers.any (· == p) then
    let newPubs := s.publishers.filter (· != p)
    (RemoveResult.ok newPubs, { s with publishers := newPubs })
  else
    (RemoveResult.errPublisherNotFound, s)

/-! ## Open/Close lifecycle -/

/-- Result of lifecycle transitions. -/
inductive LifecycleResult where
  | ok
  | errAlreadyOpen
  | errAlreadyClosed
  deriving Repr, DecidableEq

/--
  Mirrors `openNodeLocked` from route.go lines 106-129.
  If already open, returns errAlreadyOpen.
  Otherwise sets isOpen to true.
-/
def RouteState.openNode (s : RouteState) : LifecycleResult × RouteState :=
  if s.isOpen then
    (LifecycleResult.errAlreadyOpen, s)
  else
    (LifecycleResult.ok, { s with isOpen := true })

/--
  Mirrors `closeNodeLocked` from route.go lines 131-154.
  If already closed, returns errAlreadyClosed.
  Otherwise sets isOpen to false.
-/
def RouteState.closeNode (s : RouteState) : LifecycleResult × RouteState :=
  if s.isOpen then
    (LifecycleResult.ok, { s with isOpen := false })
  else
    (LifecycleResult.errAlreadyClosed, s)

/-! ## GetRoute modes (simplified, pure map logic) -/

/-- Mirrors Go's `GetRouteMode` enum (simplified to pure/synchronous modes). -/
inductive GetRouteMode where
  | failIfNotFound
  | createIfNotFound
  | createAlways
  deriving Repr, DecidableEq

/-- Result of getRoute. -/
inductive GetRouteResult where
  | found (path : String)
  | created (path : String)
  | errNotFound
  | errAlreadyExists
  deriving Repr, DecidableEq

/-- A route map: list of path strings (abstracting away the map). -/
abbrev RouteMap := List String

/--
  Mirrors the synchronous subset of `getRouteLocked` from router.go.
  - failIfNotFound: returns found if present, errNotFound otherwise.
  - createIfNotFound: returns found if present, creates otherwise.
  - createAlways: errors if present, creates otherwise.
-/
def getRoute (routes : RouteMap) (path : String) (mode : GetRouteMode) : GetRouteResult × RouteMap :=
  let pathExists := routes.any (· == path)
  match mode with
  | GetRouteMode.failIfNotFound =>
    if pathExists then (GetRouteResult.found path, routes)
    else (GetRouteResult.errNotFound, routes)
  | GetRouteMode.createIfNotFound =>
    if pathExists then (GetRouteResult.found path, routes)
    else (GetRouteResult.created path, path :: routes)
  | GetRouteMode.createAlways =>
    if pathExists then (GetRouteResult.errAlreadyExists, routes)
    else (GetRouteResult.created path, path :: routes)

/-- Add a route to the map. -/
def addRoute (routes : RouteMap) (path : String) : RouteMap := path :: routes

/-- Remove a route from the map. -/
def removeRoute (routes : RouteMap) (path : String) : RouteMap := routes.filter (· != path)
