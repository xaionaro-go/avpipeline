-- Spec/LimitBitrate.lean: Formal specification of the LimitVideoBitrate filter
-- from packetorframe/filter/limitvideobitrate/filter.go

/-!
  Models the bitrate-limiting filter as a leaky-bucket state machine.

  The Go filter tracks `videoAveragerBufferConsumed` (accumulated bits in the
  bucket). On each video packet arrival:

  1. Drain: subtract bits allowed by elapsed time, clamp to 0.
  2. Compute `consumedWithPacket` = consumed + packetSizeBits.
  3. Reject if consumedWithPacket > averagingBuffer, unless it is a keyframe
     and the bucket was empty before adding this packet.
  4. Reject non-keyframes when we are in a skip streak (only a keyframe resets it).
  5. Accept: update consumed, clear skip streak.

  We model time differences and bitrates as natural numbers (bits, bits/sec,
  seconds expressed as Nat) to stay in decidable integer arithmetic.
-/

/-- Input classification for the filter. -/
inductive InputKind where
  | nonVideo          -- non-video media type → always passes
  | videoFrame        -- video but decoded frame, not packet → always passes
  | videoPacket (sizeBits : Nat) (isKeyFrame : Bool)
  deriving Repr, DecidableEq

/-- Filter configuration (immutable). -/
structure LimitBitrateConfig where
  averageBitRate         : Nat   -- bits per second (0 = disabled)
  averagingBufferBits    : Nat   -- BitrateAveragingPeriod_seconds * averageBitRate
  deriving Repr, DecidableEq

/-- Mutable filter state. -/
structure LimitBitrateState where
  consumed        : Nat    -- videoAveragerBufferConsumed (bits in bucket)
  skippedFrame    : Bool   -- whether we are in a skip streak
  deriving Repr, DecidableEq

namespace LimitBitrateState

def init : LimitBitrateState := ⟨0, false⟩

/--
  Drain the bucket by the number of bits allowed since the last packet.
  `allowedBits` models `1 + tsDiff.Seconds() * averageBitRate` from Go.
  We clamp to 0 (Go: `if consumed < 0 { consumed = 0 }`).
-/
def drain (s : LimitBitrateState) (allowedBits : Nat) : LimitBitrateState :=
  { s with consumed := s.consumed - allowedBits }  -- Nat subtraction clamps to 0

/--
  Core decision for a video packet, mirroring `sendInputVideo` lines 86-104.
  Returns (accepted, newState).
-/
def processVideoPacket (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (sizeBits : Nat) (isKeyFrame : Bool) : Bool × LimitBitrateState :=
  let consumedWithPacket := s.consumed + sizeBits
  -- Condition 1: bucket overflow check
  let overflows := consumedWithPacket > cfg.averagingBufferBits
  -- Go: (!isKeyFrame || consumed != 0)
  let notKeyFrameOrBucketNonEmpty := !isKeyFrame || (s.consumed != 0)
  let rejectOverflow := overflows && notKeyFrameOrBucketNonEmpty
  if rejectOverflow then
    -- Reject: mark skip streak, don't update consumed
    (false, { s with skippedFrame := true })
  else if s.skippedFrame && !isKeyFrame then
    -- In skip streak, only keyframes can break out
    (false, s)
  else
    -- Accept
    (true, { consumed := consumedWithPacket, skippedFrame := false })

/--
  Top-level filter match function, mirroring `Match` + `sendInputVideo`.
  `allowedBits` is the drain amount from elapsed time.
-/
def step (s : LimitBitrateState) (cfg : LimitBitrateConfig)
    (input : InputKind) (allowedBits : Nat) : Bool × LimitBitrateState :=
  -- Zero bitrate means no limiting
  if cfg.averageBitRate == 0 then
    (true, s)
  else
    match input with
    | InputKind.nonVideo => (true, s)
    | InputKind.videoFrame => (true, s)
    | InputKind.videoPacket sizeBits isKeyFrame =>
      let drained := s.drain allowedBits
      drained.processVideoPacket cfg sizeBits isKeyFrame

end LimitBitrateState
