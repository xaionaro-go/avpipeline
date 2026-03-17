-- Spec/StreamMux/System.lean: System-level state machine model for the
-- streammux auto-bitrate control loop.
--
-- Models MULTIPLE INTERACTING COMPONENTS as a composed state machine:
--   - Measurement subsystem (per media type)
--   - QueueSizeGapDecay calculator
--   - Resolution control with slowdown
--   - Bitrate clamping
--   - FPS reduction
--
-- All arithmetic uses Int (no Float). Divisions guarded by explicit non-zero proofs.

import Spec.StreamMux.QueueSizeGapDecay
import Spec.StreamMux.BitrateControl
import Spec.StreamMux.Smoothing

set_option linter.unusedVariables false

/-! # Media types -/

/-- Media type tag, mirroring astiav.MediaType. -/
inductive SysMediaType where
  | video
  | audio
  deriving Repr, DecidableEq, BEq

/-! # Measurement state -/

/-- Per-track measurement state. All values in bits/s or nanoseconds. -/
structure SysMeasurementState where
  inputBitRate   : Int  -- bits/s
  encodedBitRate : Int  -- bits/s
  outputBitRate  : Int  -- bits/s
  sendingLatency : Int  -- nanoseconds
  deriving Repr, DecidableEq

/-! # Resolution -/

/-- A resolution (width x height). -/
structure SysResolution where
  width  : Int
  height : Int
  deriving Repr, DecidableEq, BEq

def SysResolution.pixels (r : SysResolution) : Int := r.width * r.height

/-! # System state -/

/-- A pending resolution change request. -/
structure SysResChangeRequest where
  isUpgrade : Bool
  startedAt : Int  -- ms timestamp
  latestAt  : Int  -- ms timestamp
  deriving Repr, DecidableEq

/-- Full system state for the auto-bitrate control loop. -/
structure SystemState where
  -- Measurement state per media type
  videoMeasurements : SysMeasurementState
  audioMeasurements : SysMeasurementState

  -- Calculator state
  lastTotalQueueSize : Int   -- bytes
  lastVideoBitRate   : Int   -- bits/s
  lastCheckTS        : Int   -- ms timestamp

  -- Resolution control
  currentResChangeReq   : Option SysResChangeRequest
  temporaryFPSMultNum   : Int  -- FPS reduction multiplier numerator
  temporaryFPSMultDen   : Int  -- FPS reduction multiplier denominator
  lastFPSUpdateTS       : Int  -- ms timestamp
  bypassEnabled         : Bool

  -- Configuration (immutable across transitions but part of state for modeling)
  minBitRate         : Int  -- bits/s
  maxBitRate         : Int  -- bits/s
  upgradeSlowdownMs  : Int  -- ms
  downgradeSlowdownMs : Int -- ms

  -- Output state
  currentResolution : SysResolution
  currentBitrate    : Int  -- bits/s
  deriving Repr, DecidableEq

/-! # Well-formedness predicates -/

/-- A system state is well-formed when configuration values are positive
    and measurement values are non-negative. -/
def SystemState.wellFormed (s : SystemState) : Prop :=
  s.minBitRate > 0 ∧
  s.maxBitRate > 0 ∧
  s.minBitRate ≤ s.maxBitRate ∧
  s.upgradeSlowdownMs > 0 ∧
  s.downgradeSlowdownMs > 0 ∧
  s.currentBitrate > 0 ∧
  s.videoMeasurements.outputBitRate ≥ 0 ∧
  s.videoMeasurements.inputBitRate ≥ 0 ∧
  s.videoMeasurements.encodedBitRate ≥ 0 ∧
  s.videoMeasurements.sendingLatency ≥ 0 ∧
  s.audioMeasurements.outputBitRate ≥ 0 ∧
  s.audioMeasurements.inputBitRate ≥ 0 ∧
  s.audioMeasurements.sendingLatency ≥ 0 ∧
  s.temporaryFPSMultDen > 0 ∧
  s.temporaryFPSMultNum > 0 ∧
  s.lastTotalQueueSize ≥ 0 ∧
  s.lastVideoBitRate ≥ 0

/-! # Safe division -/

/-- Safe division that returns `none` when the denominator is zero.
    Models the Inf-producing path in Go floating-point arithmetic. -/
def safeDiv (a b : Int) : Option Int :=
  if b = 0 then none else some (a / b)

/-! # Transition 1: Measurement update -/

/-- Input for measurement tick. -/
structure MeasurementInput where
  mediaType         : SysMediaType
  newInputBitRate   : Int
  newEncodedBitRate : Int
  newOutputBitRate  : Int
  inertiaNum        : Nat  -- e.g., 9 for 0.9
  inertiaDen        : Nat  -- e.g., 10 for 0.9
  count             : Nat  -- measurement count
  deriving Repr

/-- Apply measurement update to the system state.
    Models one iteration of the measurement loop in inputBitRateMeasurerLoop. -/
def measureBitRates (s : SystemState) (inp : MeasurementInput) : SystemState :=
  let m := match inp.mediaType with
    | SysMediaType.video => s.videoMeasurements
    | SysMediaType.audio => s.audioMeasurements
  let den := StreamMux.smoothedDen inp.inertiaDen inp.count
  let newInput := if den = 0 then m.inputBitRate
    else (StreamMux.smoothedNum m.inputBitRate.toNat inp.newInputBitRate.toNat
            inp.inertiaNum inp.inertiaDen inp.count : Int) / den
  let newEncoded := if den = 0 then m.encodedBitRate
    else (StreamMux.smoothedNum m.encodedBitRate.toNat inp.newEncodedBitRate.toNat
            inp.inertiaNum inp.inertiaDen inp.count : Int) / den
  let newOutput := if den = 0 then m.outputBitRate
    else (StreamMux.smoothedNum m.outputBitRate.toNat inp.newOutputBitRate.toNat
            inp.inertiaNum inp.inertiaDen inp.count : Int) / den
  let newM : SysMeasurementState := {
    inputBitRate := newInput,
    encodedBitRate := newEncoded,
    outputBitRate := newOutput,
    sendingLatency := m.sendingLatency
  }
  match inp.mediaType with
  | SysMediaType.video => { s with videoMeasurements := newM }
  | SysMediaType.audio => { s with audioMeasurements := newM }

/-! # Transition 2: Latency measurement -/

/-- Input for latency measurement tick. -/
structure LatencyInput where
  videoOldestDTS   : Int  -- nanoseconds
  videoEarliestDTS : Int  -- nanoseconds
  audioOldestDTS   : Int  -- nanoseconds
  audioEarliestDTS : Int  -- nanoseconds
  deriving Repr

/-- Compute latency from DTS values. Models stream_mux.go:1527-1533. -/
def sysComputeLatency (earliestDTS oldestDTS : Int) : Int :=
  if oldestDTS > 0 then max 0 (earliestDTS - oldestDTS)
  else 0

/-- INV2 key model: latency measurement reads from the CORRECT media type.
    Video latency reads from video output (videoOldestDTS, videoEarliestDTS).
    Audio latency reads from audio output (audioOldestDTS, audioEarliestDTS). -/
def measureLatency (s : SystemState) (inp : LatencyInput) : SystemState :=
  let videoLatency := sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS
  let audioLatency := sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS
  { s with
    videoMeasurements := { s.videoMeasurements with sendingLatency := videoLatency },
    audioMeasurements := { s.audioMeasurements with sendingLatency := audioLatency }
  }

/-- A BUGGY version of measureLatency where audio reads from video DTS.
    This models the outputVideo/outputAudio bug. INV2 should FAIL on this. -/
def measureLatencyBuggy (s : SystemState) (inp : LatencyInput) : SystemState :=
  let videoLatency := sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS
  -- BUG: audio reads video DTS instead of audio DTS
  let audioLatency := sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS
  { s with
    videoMeasurements := { s.videoMeasurements with sendingLatency := videoLatency },
    audioMeasurements := { s.audioMeasurements with sendingLatency := audioLatency }
  }

/-! # Transition 3: checkOnce (bitrate check loop iteration) -/

/-- Input for one checkOnce iteration. -/
structure CheckOnceInput where
  nowMs              : Int   -- current timestamp in ms
  totalQueueSize     : Int   -- bytes (from calculateTotalQueueSize)
  gapDecayConfig     : GapDecayConfig
  checkIntervalMs    : Int   -- ms (must be > 0)
  smoothedDerivative : Int   -- bytes/s (from moving average)
  deriving Repr

/-- Compute new bitrate from the GapDecay calculator with inertia. -/
def computeNewBitrate (cfg : GapDecayConfig) (req : GapDecayRequest) : Int :=
  let raw := GapDecaySpec.computeRawNewBitRate cfg req
  GapDecaySpec.applyInertia raw req.currentBitRate req.checkInterval
    cfg.inertiaIncrease cfg.inertiaDecrease

/-- Clamp a bitrate to the configured range.
    Models the clamping in trySetVideoBitrate. -/
def clampBitrateSystem (s : SystemState) (bitrate : Int) : Int :=
  let inputBR := s.videoMeasurements.inputBitRate
  clampBitRate s.minBitRate s.maxBitRate inputBR bitrate

/-- Full checkOnce transition.
    Models one iteration of the periodic bitrate check loop. -/
def sysCheckOnce (s : SystemState) (inp : CheckOnceInput) : SystemState :=
  let tsDiffMs := inp.nowMs - s.lastCheckTS
  if tsDiffMs ≤ 0 then s
  else if s.currentBitrate ≤ 0 then s
  else
    let req : GapDecayRequest := {
      currentBitRate := s.currentBitrate,
      inputBitRate := s.videoMeasurements.inputBitRate,
      actualOutputBitRate := s.videoMeasurements.outputBitRate,
      queueDuration := s.videoMeasurements.sendingLatency / 1000000000,
      actualDerivative := inp.smoothedDerivative,
      checkInterval := inp.checkIntervalMs
    }
    let newBR := computeNewBitrate inp.gapDecayConfig req
    if newBR ≤ 0 then
      { s with lastCheckTS := inp.nowMs, lastTotalQueueSize := inp.totalQueueSize }
    else
      let clamped := clampBitrateSystem s newBR
      { s with
        currentBitrate := clamped,
        lastCheckTS := inp.nowMs,
        lastTotalQueueSize := inp.totalQueueSize,
        lastVideoBitRate := s.videoMeasurements.outputBitRate
      }

/-! # Reachability -/

/-- An input event to the system. -/
inductive SystemInput where
  | measureBitRatesEvt (inp : MeasurementInput)
  | measureLatencyEvt (inp : LatencyInput)
  | checkOnceEvt (inp : CheckOnceInput)
  deriving Repr

/-- Apply one transition. -/
def sysStep (s : SystemState) (inp : SystemInput) : SystemState :=
  match inp with
  | SystemInput.measureBitRatesEvt i => measureBitRates s i
  | SystemInput.measureLatencyEvt i => measureLatency s i
  | SystemInput.checkOnceEvt i => sysCheckOnce s i

/-- A state is reachable from an initial state via a sequence of inputs. -/
inductive Reachable (init : SystemState) : SystemState → Prop where
  | base : Reachable init init
  | step : Reachable init s → (inp : SystemInput) → Reachable init (sysStep s inp)

/-! # Initial state constructor -/

/-- Create an initial well-formed system state. -/
def mkInitialState (minBR maxBR : Int) (upgradeMs downgradeMs : Int)
    (initBitrate : Int) (initRes : SysResolution)
    : SystemState :=
  { videoMeasurements := { inputBitRate := 0, encodedBitRate := 0, outputBitRate := 0, sendingLatency := 0 },
    audioMeasurements := { inputBitRate := 0, encodedBitRate := 0, outputBitRate := 0, sendingLatency := 0 },
    lastTotalQueueSize := 0,
    lastVideoBitRate := 0,
    lastCheckTS := 0,
    currentResChangeReq := none,
    temporaryFPSMultNum := 1,
    temporaryFPSMultDen := 1,
    lastFPSUpdateTS := 0,
    bypassEnabled := false,
    minBitRate := minBR,
    maxBitRate := maxBR,
    upgradeSlowdownMs := upgradeMs,
    downgradeSlowdownMs := downgradeMs,
    currentResolution := initRes,
    currentBitrate := initBitrate }

/-! # INV1: Numerical Safety — denominators in the computation chain are non-zero -/

/-- The GapDecay calculator's computation chain involves division by gapDecay.
    This predicate captures that gapDecay > 0 (non-zero denominator). -/
def GapDecayConfig.safe (cfg : GapDecayConfig) : Prop :=
  cfg.gapDecay > 0 ∧ cfg.inertiaIncrease > 0 ∧ cfg.inertiaDecrease > 0

/-- The derivative computation's denominator (tsDiffMs) is safe when > 0. -/
def derivativeDenomSafe (nowMs lastCheckTS : Int) : Prop :=
  nowMs - lastCheckTS > 0

/-- The inertial smoothing denominator is non-zero when inertiaDen > 0. -/
def smoothingDenomSafe (inertiaDen : Nat) (count : Nat) : Prop :=
  StreamMux.smoothedDen inertiaDen count > 0

/-! # INV2: Data Flow Correctness -/

/-- Data flow correctness: measureLatency writes audio latency from audio DTS inputs
    and video latency from video DTS inputs. -/
def dataFlowCorrect (s : SystemState) (inp : LatencyInput) : Prop :=
  let s' := measureLatency s inp
  s'.audioMeasurements.sendingLatency = sysComputeLatency inp.audioEarliestDTS inp.audioOldestDTS ∧
  s'.videoMeasurements.sendingLatency = sysComputeLatency inp.videoEarliestDTS inp.videoOldestDTS

/-! # INV3: Resolution Stability -/

/-- Resolution stability: if a request started less than slowdownMs ago,
    checkSlowdown blocks the resolution change (returns notThisTime). -/
def resolutionStable (prevStartedAt nowMs slowdownMs : Int) : Prop :=
  nowMs - prevStartedAt < slowdownMs →
  ∀ isUpgrade targetPx avgPx,
    checkSlowdown
      (some ⟨isUpgrade, prevStartedAt, nowMs⟩)
      isUpgrade (nowMs + 1) slowdownMs slowdownMs targetPx avgPx =
    SlowdownResult.notThisTime ⟨isUpgrade, prevStartedAt, nowMs + 1⟩

/-! # INV4: Derivative Bounded -/

/-- Queue size derivative is bounded: the Euclidean quotient satisfies
    0 ≤ remainder < divisor, so q/d is at most q/d in absolute terms.
    For non-negative numerator and positive denominator:
    0 ≤ (a / b) * b ≤ a.
    This is the key property that prevents derivative blow-up. -/
def derivativeBoundedNonneg (a b : Int) : Prop :=
  a ≥ 0 → b > 0 → a / b * b ≤ a ∧ 0 ≤ a / b

/-- Derivative is zero when queue sizes are equal (no change). -/
def derivativeZeroWhenEqual (queue tsDiffMs : Int) : Prop :=
  tsDiffMs > 0 → (queue - queue) * 1000 / tsDiffMs = 0

/-- Derivative sign matches queue change direction. -/
def derivativeSignCorrect (newQueue oldQueue tsDiffMs : Int) : Prop :=
  tsDiffMs > 0 → newQueue > oldQueue → (newQueue - oldQueue) * 1000 / tsDiffMs ≥ 0

/-! # INV5: Bitrate Convergence Under Constant Conditions -/

/-- Equilibrium: when queue is at optimal with zero derivative, bitrate doesn't change.
    The GapDecay calculator produces bitRateDiff = 0 under these conditions. -/
def atEquilibrium (cfg : GapDecayConfig) (req : GapDecayRequest) : Prop :=
  req.queueDuration = GapDecaySpec.queueDurationOptimal cfg req.actualOutputBitRate ∧
  req.actualDerivative = 0

/-! # INV6: Measurement Monotonicity (convex combination) -/

/-- The inertial smoothing produces a value between old and new (as a fraction).
    For each measurement value, the update is a convex combination. -/
def measurementConvex (old new_ : Nat) (inertiaNum inertiaDen count : Nat) : Prop :=
  inertiaNum * count ≤ inertiaDen * (count + 3) →
  let den := StreamMux.smoothedDen inertiaDen count
  let num := StreamMux.smoothedNum old new_ inertiaNum inertiaDen count
  min old new_ * den ≤ num ∧ num ≤ max old new_ * den
