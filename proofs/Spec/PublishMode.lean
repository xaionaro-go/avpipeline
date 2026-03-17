-- Spec/PublishMode.lean: Formal specification of router publish modes from router/publish_mode.go

/-- Mirrors Go's `PublishMode` enum. -/
inductive PublishMode where
  | undefined
  | exclusiveTakeover
  | exclusiveFail
  | sharedTakeover
  | sharedFail
  deriving Repr, DecidableEq

namespace PublishMode

/-- Mirrors Go's `IsExclusive()` method. -/
def isExclusive : PublishMode → Bool
  | exclusiveTakeover => true
  | exclusiveFail => true
  | _ => false

/-- Mirrors Go's `FailOnConflict()` method. -/
def failOnConflict : PublishMode → Bool
  | exclusiveFail => true
  | sharedFail => true
  | _ => false

end PublishMode
