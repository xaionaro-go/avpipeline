-- Spec/LimitFramerate.lean: Formal specification of LimitFramerate filter from
-- packetorframe/filter/limitframerate/limit_framerate.go

import Spec.Rational

/-- Per-stream state for the limit-framerate filter.
    Models the per-stream maps: NextMinPTS, PTSDebt, DurRemainder. -/
structure LimitState where
  minPTS      : Int   -- next minimum PTS for acceptance
  debt        : Int   -- accumulated PTS debt (≥ 0)
  durRemainder : Int  -- duration remainder for accurate duration
  deriving Repr, DecidableEq

namespace LimitState

def init : LimitState := ⟨0, 0, 0⟩

/-- Input to the match function. -/
structure Input where
  pts : Int
  dur : Int
  deriving Repr

-- minDuration is a Rational computed from timeBase and maxFPS.
-- Go: `minDuration := timeBase.Reverse().Div(maxFPS)`
-- We take it as a parameter since it depends on timeBase which is external.

/-- Result of the match function: accepted or rejected, plus the new state
    and (if accepted) the assigned duration. -/
structure MatchResult where
  accepted     : Bool
  newState     : LimitState
  assignedDur  : Int   -- only meaningful when accepted
  deriving Repr

/-- Pure model of the `match` function.
    minDuration is the pre-computed Rational (minDuration.num / minDuration.den).
    Preconditions: maxFPSNum > 0, minDuration.num > 0, minDuration.den > 0. -/
def match' (s : LimitState) (inp : Input) (minDuration : Rational)
    (maxFPSNum : Int) : MatchResult :=
  -- Step 1: maxFPS.Num == 0 → reject all
  if maxFPSNum == 0 then
    { accepted := false, newState := s, assignedDur := inp.dur }
  -- Step 2: large backward jump
  else if inp.pts < s.minPTS - 2 && s.minPTS >= 2 then
    { accepted := false, newState := s, assignedDur := inp.dur }
  -- Step 3: debt tracking when pts < minPTS
  else if inp.pts < s.minPTS then
    let addDebt := s.minPTS - inp.pts
    let totalDebt := s.debt + addDebt
    if totalDebt > 3 then
      { accepted := false,
        newState := { s with debt := totalDebt },
        assignedDur := inp.dur }
    else
      -- Accepted despite pts < minPTS, debt remains ≤ 3
      -- debt update: debt -= (pts - minPTS) = debt + (minPTS - pts) = debt + addDebt = totalDebt
      -- But wait: the Go code falls through to line 106: debt -= int64(pts - minPTS)
      -- Since pts < minPTS, pts - minPTS is negative, so debt -= negative = debt += |diff|
      -- Actually in Go int64 arithmetic: debt -= int64(pts - minPTS) where pts - minPTS < 0
      -- So newDebt = debt - (pts - minPTS) = debt + (minPTS - pts) = totalDebt
      -- Then if newDebt <= 0 → delete, else store
      let newDebt := s.debt - (inp.pts - s.minPTS)  -- = debt + (minPTS - pts)
      let debtStored := if newDebt <= 0 then 0 else newDebt
      -- Compute nextMinPTS
      let effectivePTS := max inp.pts s.minPTS
      let curFrameID := effectivePTS * minDuration.den / minDuration.num
      let nextMinPTS := (curFrameID + 1) * minDuration.num / minDuration.den
      -- Duration
      let num := minDuration.num + s.durRemainder
      let minDurationInt := num / minDuration.den
      let newDurRemainder := num % minDuration.den
      let assignedDur := if inp.dur < minDurationInt + 3 then minDurationInt else inp.dur
      { accepted := true,
        newState := { minPTS := nextMinPTS, debt := debtStored, durRemainder := newDurRemainder },
        assignedDur := assignedDur }
  else
    -- pts >= minPTS: normal acceptance path
    let newDebt := s.debt - (inp.pts - s.minPTS)
    let debtStored := if newDebt <= 0 then 0 else newDebt
    -- Compute nextMinPTS
    let effectivePTS := max inp.pts s.minPTS
    let curFrameID := effectivePTS * minDuration.den / minDuration.num
    let nextMinPTS := (curFrameID + 1) * minDuration.num / minDuration.den
    -- Duration
    let num := minDuration.num + s.durRemainder
    let minDurationInt := num / minDuration.den
    let newDurRemainder := num % minDuration.den
    let assignedDur := if inp.dur < minDurationInt + 3 then minDurationInt else inp.dur
    { accepted := true,
      newState := { minPTS := nextMinPTS, debt := debtStored, durRemainder := newDurRemainder },
      assignedDur := assignedDur }

/-- Convenience: is the frame accepted? -/
def isAccepted (s : LimitState) (inp : Input) (minDur : Rational) (maxFPSNum : Int) : Bool :=
  (s.match' inp minDur maxFPSNum).accepted

/-- Convenience: get new state after match. -/
def nextState (s : LimitState) (inp : Input) (minDur : Rational) (maxFPSNum : Int) : LimitState :=
  (s.match' inp minDur maxFPSNum).newState

end LimitState
