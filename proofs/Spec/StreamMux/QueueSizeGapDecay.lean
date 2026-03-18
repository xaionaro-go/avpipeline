-- Spec/StreamMux/QueueSizeGapDecay.lean: Formal specification of the
-- QueueSizeGapDecay auto-bitrate calculator from
-- preset/streammux/types/autobitrate_calculator_queue_size_gap_decay.go

/-!
  Models the proportional-control bitrate calculator.

  All quantities are Int to stay in decidable integer arithmetic.
  Time is in abstract seconds (Int), rates in bits/s or bytes/s (Int).

  Go computation chain (CalculateBitRate):
    queueDurationOptimal = max(configOptimal, minQueueBytes * 8 / outputBps)
    gap           = queueDuration - queueDurationOptimal          -- seconds
    gapB          = outputBps * gap / 8                           -- bytes
    desiredDeriv  = -(gapB / decayTime)                           -- bytes/s
    derivGap      = desiredDeriv - actualDeriv                    -- bytes/s
    bitRateDiff   = derivGap * 8                                  -- bits/s
    newBitRate    = max(currentBitRate + bitRateDiff, 1)          -- bits/s

  Inertia clamps (increase/decrease slowdown) and critical flag are modeled
  separately.
-/

/-- Calculator configuration, mirrors AutoBitrateCalculatorQueueSizeGapDecay fields. -/
structure GapDecayConfig where
  queueDurationOptimal : Int  -- seconds (from QueueDurationOptimal)
  queueSizeMinBytes    : Int  -- bytes (from QueueSizeMin)
  gapDecay             : Int  -- seconds (from GapDecay), must be > 0
  inertiaIncrease      : Int  -- seconds (from InertiaIncrease), must be > 0
  inertiaDecrease      : Int  -- seconds (from InertiaDecrease), must be > 0
  deriving Repr, DecidableEq

/-- Request inputs, mirrors CalculateBitRateRequest fields. -/
structure GapDecayRequest where
  currentBitRate      : Int  -- bits/s (CurrentBitrateSetting)
  inputBitRate        : Int  -- bits/s (InputBitrate)
  actualOutputBitRate : Int  -- bits/s (ActualOutputBitrate)
  queueDuration       : Int  -- seconds (QueueDuration)
  actualDerivative    : Int  -- bytes/s (smoothed QueueSizeDerivative)
  checkInterval       : Int  -- seconds (Config.CheckInterval), must be > 0
  deriving Repr, DecidableEq

/-- Result, mirrors BitRateChangeRequest. -/
structure GapDecayResult where
  bitRate    : Int
  isCritical : Bool
  deriving Repr, DecidableEq

namespace GapDecaySpec

/-- Effective optimal queue duration: max of configured optimal and the
    duration needed to hold minQueueBytes at the current output rate.
    Mirrors: max(US(d.QueueDurationOptimal), d.QueueSizeMin.Tob().ToS(req.ActualOutputBitrate))
    = max(configOptimal, minQueueBytes * 8 / outputBps). -/
def queueDurationOptimal (cfg : GapDecayConfig) (outputBps : Int) : Int :=
  max cfg.queueDurationOptimal (cfg.queueSizeMinBytes * 8 / outputBps)

/-- Gap between actual and optimal queue duration (seconds). -/
def gap (queueDur optimal : Int) : Int :=
  queueDur - optimal

/-- Gap converted to bytes: outputBps * gap / 8.
    Mirrors: req.ActualOutputBitrate.Tob(gap).ToB() -/
def gapBytes (outputBps gapS : Int) : Int :=
  outputBps * gapS / 8

/-- Desired derivative: -(gapB / decayTime).
    Mirrors: -gapB.ToBps(US(d.GapDecay)) -/
def desiredDerivative (gapB decayTime : Int) : Int :=
  -(gapB / decayTime)

/-- Derivative gap: desiredDeriv - actualDeriv (bytes/s). -/
def derivativeGap (desiredDeriv actualDeriv : Int) : Int :=
  desiredDeriv - actualDeriv

/-- Bit rate diff: derivativeGap * 8 (bits/s).
    Mirrors: derivativeGap.Tobps() -/
def bitRateDiff (derivGap : Int) : Int :=
  derivGap * 8

/-- Raw new bitrate before inertia clamping.
    Mirrors: max(Ubps(req.CurrentBitrateSetting) + bitRateDiff, 1) -/
def rawNewBitRate (currentBps diffBps : Int) : Int :=
  max (currentBps + diffBps) 1

/-- Full computation of bitRateDiff from inputs (convenience composite). -/
def computeBitRateDiff (cfg : GapDecayConfig) (req : GapDecayRequest) : Int :=
  let optimal := queueDurationOptimal cfg req.actualOutputBitRate
  let g := gap req.queueDuration optimal
  let gB := gapBytes req.actualOutputBitRate g
  let dd := desiredDerivative gB cfg.gapDecay
  let dg := derivativeGap dd req.actualDerivative
  bitRateDiff dg

/-- Full computation of raw new bitrate from inputs. -/
def computeRawNewBitRate (cfg : GapDecayConfig) (req : GapDecayRequest) : Int :=
  rawNewBitRate req.currentBitRate (computeBitRateDiff cfg req)

/-! ## Inertia clamping

  For increases: newBitRate / currentBitRate ≤ 1 + checkInterval / inertiaIncrease
  For decreases: currentBitRate / newBitRate ≤ 1 + checkInterval / inertiaDecrease

  We model the clamped value using integer arithmetic:
    increase cap: currentBitRate * (inertia + interval) / inertia
    decrease cap: currentBitRate * inertia / (inertia + interval)
    (with +1 for the decrease floor in Go)
-/

/-- Inertia-capped bitrate for increases.
    Go: newBitRate = float64(currentBitrateSetting) * (1 + fraction)
    = currentBitRate * (inertiaIncrease + checkInterval) / inertiaIncrease -/
def inertiaCapIncrease (current checkInterval inertiaIncrease : Int) : Int :=
  current * (inertiaIncrease + checkInterval) / inertiaIncrease

/-- Inertia-capped bitrate for decreases.
    Go: newBitRate = 1 + float64(currentBitrateSetting) / (1 + fraction)
    = 1 + currentBitRate * inertiaDecrease / (inertiaDecrease + checkInterval) -/
def inertiaCapDecrease (current checkInterval inertiaDecrease : Int) : Int :=
  1 + current * inertiaDecrease / (inertiaDecrease + checkInterval)

/-- Apply inertia clamping to the raw new bitrate.
    Go branches on the sign of `bitRateDiff` (the adjustment computed BEFORE
    the `max(...,1)` floor), not on `raw - current`. When currentBR is very
    small (e.g. 0) and bitRateDiff < 0, the floor produces raw = 1 > current,
    yet Go still takes the decrease branch. -/
def applyInertia (raw current checkInterval inertiaInc inertiaDec : Int)
    (bitRateDiff : Int := raw - current) : Int :=
  if bitRateDiff > 0 then
    -- increasing: cap at inertiaCapIncrease
    let cap := inertiaCapIncrease current checkInterval inertiaInc
    min raw cap
  else if bitRateDiff < 0 then
    -- decreasing: floor at inertiaCapDecrease
    let floor := inertiaCapDecrease current checkInterval inertiaDec
    max raw floor
  else
    raw

/-- Critical flag: true iff decreasing AND newBitrate < max(actual, input) / 5.
    Mirrors: bitRateDiff < 0 && newBitRate < max(req.ActualOutputBitrate, req.InputBitrate)/5 -/
def isCritical (diff newBR actualOutput inputBR : Int) : Bool :=
  diff < 0 && newBR < max actualOutput inputBR / 5

end GapDecaySpec
