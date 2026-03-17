-- Spec/StreamMux/Calculators.lean: Formal specifications of three auto-bitrate calculators
-- from preset/streammux/types/autobitrate_calculator_{static,thresholds,log_k}.go

/-!
  Models three bitrate calculators used in the stream mux pipeline:

  1. **Static** — always returns a fixed configured bitrate (Go: AutoBitrateCalculatorStatic).
  2. **Thresholds** — selects a multiplier k from queue-duration thresholds,
     returns currentBitrate * k (Go: AutoBitrateCalculatorThresholds).
  3. **LogK** — computes a ratio from queue gap, applies log-based smoothing,
     returns a monotonically adjusted bitrate (Go: AutoBitrateCalculatorLogK).

  All bitrates and durations are modeled as Int to avoid Float and stay decidable.
  Multipliers in Thresholds are modeled as rational scale factors (Nat numerator/denominator).
-/

-- Shared types mirroring Go's CalculateBitRateRequest and BitRateChangeRequest.

/-- Bitrate change request returned by all calculators. -/
structure BitRateChangeRequest where
  bitRate    : Int
  isCritical : Bool
  deriving Repr, DecidableEq

/-- Input request to a calculator. -/
structure CalculateBitRateRequest where
  currentBitrateSetting : Int
  inputBitrate          : Int
  actualOutputBitrate   : Int
  queueSize             : Int   -- queue size in bits
  deriving Repr, DecidableEq

/-! ## Static Calculator -/

namespace StaticCalc

/-- Static calculator: configured bitrate as a positive integer. -/
structure Config where
  bitrate : Int
  deriving Repr, DecidableEq

/-- Go: `return BitRateChangeRequest{BitRate: Ubps(d), IsCritical: true}` -/
def calculate (_cfg : Config) (_req : CalculateBitRateRequest) : BitRateChangeRequest :=
  { bitRate := _cfg.bitrate, isCritical := true }

end StaticCalc

/-! ## Thresholds Calculator -/

namespace ThresholdsCalc

/-- Threshold configuration with five queue-duration boundaries and five multipliers.
    Multipliers are modeled as rational numbers: result = currentBitrate * mulNum / mulDen.
    This avoids Float while preserving the multiplicative semantics. -/
structure Config where
  extremelyHighDuration : Int  -- Go: OutputExtremelyHighQueueSizeDuration
  veryHighDuration      : Int  -- Go: OutputVeryHighQueueSizeDuration
  highDuration          : Int  -- Go: OutputHighQueueSizeDuration
  lowDuration           : Int  -- Go: OutputLowQueueSizeDuration
  veryLowDuration       : Int  -- Go: OutputVeryLowQueueSizeDuration
  -- Multiplier k as (numerator, denominator) pairs; result = current * num / den
  extremeDecreaseNum : Int     -- e.g., 1 for 0.1
  extremeDecreaseDen : Int     -- e.g., 10
  quickDecreaseNum   : Int     -- e.g., 1 for 0.5
  quickDecreaseDen   : Int     -- e.g., 2
  decreaseNum        : Int     -- e.g., 95 for 0.95
  decreaseDen        : Int     -- e.g., 100
  increaseNum        : Int     -- e.g., 101 for 1.01
  increaseDen        : Int     -- e.g., 100
  quickIncreaseNum   : Int     -- e.g., 6 for 1.2
  quickIncreaseDen   : Int     -- e.g., 5
  deriving Repr, DecidableEq

/-- Well-formed config: thresholds are ordered and denominators are positive. -/
structure ConfigWF (cfg : Config) : Prop where
  thresh_order : cfg.veryLowDuration ≤ cfg.lowDuration ∧
                 cfg.lowDuration < cfg.highDuration ∧
                 cfg.highDuration ≤ cfg.veryHighDuration ∧
                 cfg.veryHighDuration ≤ cfg.extremelyHighDuration
  dens_pos : cfg.extremeDecreaseDen > 0 ∧ cfg.quickDecreaseDen > 0 ∧
             cfg.decreaseDen > 0 ∧ cfg.increaseDen > 0 ∧ cfg.quickIncreaseDen > 0
  decrease_nums_pos : cfg.extremeDecreaseNum > 0 ∧ cfg.quickDecreaseNum > 0 ∧
                      cfg.decreaseNum > 0 ∧ cfg.increaseNum > 0 ∧ cfg.quickIncreaseNum > 0
  -- Decrease multipliers < 1 (num < den), increase multipliers > 1 (num > den)
  extreme_lt : cfg.extremeDecreaseNum < cfg.extremeDecreaseDen
  quick_dec_lt : cfg.quickDecreaseNum < cfg.quickDecreaseDen
  dec_lt : cfg.decreaseNum < cfg.decreaseDen
  inc_gt : cfg.increaseNum > cfg.increaseDen
  quick_inc_gt : cfg.quickIncreaseNum > cfg.quickIncreaseDen

/-- Zone classification based on queue duration. Mirrors Go's `decideFloat` switch. -/
inductive Zone where
  | extremelyHigh  -- queueDuration >= extremelyHighDuration
  | veryHigh       -- queueDuration >= veryHighDuration (and < extremelyHigh)
  | high           -- queueDuration >= highDuration (and < veryHigh)
  | normal         -- lowDuration < queueDuration < highDuration
  | low            -- queueDuration <= lowDuration (and > veryLow)
  | veryLow        -- queueDuration <= veryLowDuration
  deriving Repr, DecidableEq

/-- Classify queue duration into a zone. Mirrors Go's switch in `decideFloat`.
    The Go switch evaluates cases top-to-bottom; we replicate that priority. -/
def classify (cfg : Config) (queueDuration : Int) : Zone :=
  if queueDuration ≥ cfg.extremelyHighDuration then Zone.extremelyHigh
  else if queueDuration ≥ cfg.veryHighDuration then Zone.veryHigh
  else if queueDuration ≤ cfg.veryLowDuration then Zone.veryLow
  else if queueDuration ≥ cfg.highDuration then Zone.high
  else if queueDuration ≤ cfg.lowDuration then Zone.low
  else Zone.normal

/-- Decide the multiplier (num, den) and isCritical from a zone. -/
def zoneMultiplier (cfg : Config) : Zone → Int × Int × Bool
  | Zone.extremelyHigh => (cfg.extremeDecreaseNum, cfg.extremeDecreaseDen, true)
  | Zone.veryHigh      => (cfg.quickDecreaseNum, cfg.quickDecreaseDen, true)
  | Zone.high          => (cfg.decreaseNum, cfg.decreaseDen, false)
  | Zone.low           => (cfg.increaseNum, cfg.increaseDen, false)
  | Zone.veryLow       => (cfg.quickIncreaseNum, cfg.quickIncreaseDen, false)
  | Zone.normal        => (1, 1, false)

/-- Full calculation: classify, pick multiplier, compute new bitrate.
    For normal zone (k=1), Go returns currentBitrate unchanged.
    Otherwise, Go returns currentBitrate * k. -/
def calculate (cfg : Config) (req : CalculateBitRateRequest) (queueDuration : Int)
    : BitRateChangeRequest :=
  let zone := classify cfg queueDuration
  let (num, den, crit) := zoneMultiplier cfg zone
  if num = den then
    { bitRate := req.currentBitrateSetting, isCritical := false }
  else
    { bitRate := req.currentBitrateSetting * num / den, isCritical := crit }

end ThresholdsCalc

/-! ## LogK Calculator -/

namespace LogKCalc

/-- LogK calculator configuration. All durations/rates are integers.
    `queueOptimal` and `queueDurationError` are in abstract time units. -/
structure Config where
  queueOptimal       : Int  -- target queue duration
  queueDurationError : Int  -- additive error term (Go: 20ms)
  deriving Repr, DecidableEq

/-- Well-formed config: both positive (the denominators must be nonzero). -/
structure ConfigWF (cfg : Config) : Prop where
  opt_pos : cfg.queueOptimal > 0
  err_pos : cfg.queueDurationError > 0

/--
  The ratio k = (queueOptimal + error) / (queueDuration + error).
  Since we avoid Float, we represent this as a pair (numerator, denominator)
  without dividing. The key insight for monotonicity: as queueDuration increases,
  the denominator increases, so k decreases.
-/
def kRatio (cfg : Config) (queueDuration : Int) : Int × Int :=
  (cfg.queueOptimal + cfg.queueDurationError, queueDuration + cfg.queueDurationError)

/--
  Abstract model of the LogK step function. Instead of modeling Float log/moving-average,
  we capture the structural property: the calculator produces a bitrate that is a
  monotone non-decreasing function of k = (optimal + err) / (actual + err).

  Given a "smoothed k" value represented as (kNum, kDen), the Go code computes:
    diff = currentBitrate * log(kSmoothed) * (1 - inertia)
    newBitRate = max(currentBitrate + diff, 1)

  When kSmoothed > 1 (queue smaller than optimal): log > 0, diff > 0 → bitrate increases.
  When kSmoothed < 1 (queue larger than optimal): log < 0, diff < 0 → bitrate decreases.
  When kSmoothed = 1: diff = 0 → bitrate unchanged.

  We model this as: the sign of the change tracks the sign of (kNum - kDen),
  and the result is always >= 1.
-/
def signOfChange (kNum kDen : Int) : Int :=
  if kNum > kDen then 1       -- queue below optimal → increase
  else if kNum < kDen then -1  -- queue above optimal → decrease
  else 0                       -- at optimal → no change

/-- Output bitrate is always at least 1 (Go: `max(..., 1)`). -/
def clampMin (v : Int) : Int :=
  if v < 1 then 1 else v

end LogKCalc
