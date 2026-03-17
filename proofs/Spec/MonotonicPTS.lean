-- Spec/MonotonicPTS.lean: Formal specification of MonotonicPTS filter from
-- packetorframe/filter/monotonicpts/filter.go

/-- Per-source PTS shift, keyed by stream source. We abstract source as Nat. -/
abbrev SourceKey := Nat

/-- Filter state for the monotonic PTS filter. -/
structure MonotonicPTSState where
  latestPTS : Int
  sourcePTSShift : SourceKey → Int
  shouldCorrect : Bool

namespace MonotonicPTSState

def init (shouldCorrect : Bool) : MonotonicPTSState :=
  { latestPTS := 0
    sourcePTSShift := fun _ => 0
    shouldCorrect := shouldCorrect }

/-- Input to the match function. All PTS/DTS values in timebase-integer units. -/
structure FrameInput where
  pts : Int
  dts : Int
  streamIndex : Nat
  sourceKey : SourceKey
  dtsIsNoPTS : Bool  -- models dts == NoPtsValue
  deriving Repr

/-- Result of the match function. -/
inductive MatchResult where
  | rejected
  | accepted (newPTS : Int)
  deriving Repr, DecidableEq

/-- Whether the input has an invalid PTS < DTS condition. -/
def ptsLtDts (inp : FrameInput) : Prop :=
  inp.pts < inp.dts ∧ inp.dtsIsNoPTS = false

instance (inp : FrameInput) : Decidable (ptsLtDts inp) :=
  inferInstanceAs (Decidable (inp.pts < inp.dts ∧ inp.dtsIsNoPTS = false))

/-- The shifted PTS after applying stored shift for the input's source. -/
def shiftedPTS (s : MonotonicPTSState) (inp : FrameInput) : Int :=
  inp.pts + s.sourcePTSShift inp.sourceKey

/-- Whether the shifted PTS is within tolerance of latestPTS (forward). -/
def isForward (s : MonotonicPTSState) (inp : FrameInput) (tolerance : Int) : Prop :=
  s.shiftedPTS inp + tolerance > s.latestPTS

instance (s : MonotonicPTSState) (inp : FrameInput) (tolerance : Int) :
    Decidable (isForward s inp tolerance) :=
  inferInstanceAs (Decidable (s.shiftedPTS inp + tolerance > s.latestPTS))

/--
  Mirrors the core logic of `match` from filter.go lines 53-108.
  Returns the match result (accepted/rejected).
-/
def matchResult (s : MonotonicPTSState) (inp : FrameInput) (tolerance : Int) : MatchResult :=
  if ptsLtDts inp then MatchResult.rejected
  else if inp.streamIndex ≠ 0 then MatchResult.accepted (s.shiftedPTS inp)
  else if isForward s inp tolerance then MatchResult.accepted (s.shiftedPTS inp)
  else if ¬s.shouldCorrect then MatchResult.rejected
  else MatchResult.accepted (s.latestPTS + 1)

/--
  Mirrors the state transition of `match` from filter.go lines 53-108.
  Returns the new state after the match.
-/
def matchNextState (s : MonotonicPTSState) (inp : FrameInput) (tolerance : Int) : MonotonicPTSState :=
  if ptsLtDts inp then s
  else if inp.streamIndex ≠ 0 then s
  else if isForward s inp tolerance then { s with latestPTS := s.shiftedPTS inp }
  else if ¬s.shouldCorrect then s
  else
    { s with
      latestPTS := s.latestPTS + 1
      sourcePTSShift := fun k =>
        if k = inp.sourceKey then s.latestPTS + 1 - inp.pts
        else s.sourcePTSShift k }

/-- Combined match function (equivalent to Go's match method). -/
def match' (s : MonotonicPTSState) (inp : FrameInput) (tolerance : Int)
    : MatchResult × MonotonicPTSState :=
  (s.matchResult inp tolerance, s.matchNextState inp tolerance)

end MonotonicPTSState
