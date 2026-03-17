-- Spec/StreamMux/Validation.lean: Formal specifications for five StreamMux components.
-- Models Go code from sender_key.go, codec_resource_manager.go,
-- autobitrate_handler.go, stream_mux.go, and autobitrate_video_config.go.

/-!
  ## 1. SenderKey Comparison (sender_key.go:45-77)

  Lexicographic comparison with codec-copy preference:
    1. Video codec: if different, "copy" wins (returns 1 / -1).
    2. Video resolution (width*height): higher wins.
    3. Audio codec: if different, "copy" wins.
    4. Audio sample rate: higher wins.
    5. Otherwise 0.

  ## 2. Codec Resource Reuse (codec_resource_manager.go:69-108)

  canReuse returns true iff width, height, and pixel format all match.

  ## 3. Queue Size Estimation (autobitrate_handler.go:408-458)

  On timeout for the active output:
    estimate = prevQueue + timeDelta * bitRate / 8

  ## 4. Latency Estimation (stream_mux.go:1496-1597)

  Normal: latency = max(0, earliestDTS - oldestDTS).
  Fallback on error: newLatency = prevLatency + timeDelta.

  ## 5. AutoBitRateVideoConfig (autobitrate_video_config.go)

  Resolution configs sorted by pixel count.
  Find returns matching width×height.
  Best/Worst return max/min pixel count entries.
-/

namespace Validation

/-! ### 1. SenderKey Comparison -/

/-- Whether a codec name is "copy" (lossless passthrough). -/
structure SenderKey where
  videoCodecIsCopy : Bool
  videoCodec       : Int   -- ordinal for non-copy codec names
  videoWidth       : Int
  videoHeight      : Int
  audioCodecIsCopy : Bool
  audioCodec       : Int
  audioSampleRate  : Int
  deriving Repr, DecidableEq

/-- Compare two SenderKeys, mirroring Go SenderKey.Compare.
    Returns -1, 0, or 1. -/
def SenderKey.compare (a b : SenderKey) : Int :=
  -- Step 1: video codec preference (copy wins)
  if a.videoCodec ≠ b.videoCodec ∨ a.videoCodecIsCopy ≠ b.videoCodecIsCopy then
    if a.videoCodecIsCopy then 1
    else if b.videoCodecIsCopy then -1
    else
      -- Step 2: compare by resolution pixels
      let resA := a.videoWidth * a.videoHeight
      let resB := b.videoWidth * b.videoHeight
      if resA > resB then 1
      else if resA < resB then -1
      else
        -- Step 3: audio codec preference
        if a.audioCodec ≠ b.audioCodec ∨ a.audioCodecIsCopy ≠ b.audioCodecIsCopy then
          if a.audioCodecIsCopy then 1
          else if b.audioCodecIsCopy then -1
          else
            -- Step 4: audio sample rate
            if a.audioSampleRate > b.audioSampleRate then 1
            else if a.audioSampleRate < b.audioSampleRate then -1
            else 0
        else
          -- Step 4: audio sample rate
          if a.audioSampleRate > b.audioSampleRate then 1
          else if a.audioSampleRate < b.audioSampleRate then -1
          else 0
  else
    -- Step 2: compare by resolution pixels
    let resA := a.videoWidth * a.videoHeight
    let resB := b.videoWidth * b.videoHeight
    if resA > resB then 1
    else if resA < resB then -1
    else
      -- Step 3: audio codec preference
      if a.audioCodec ≠ b.audioCodec ∨ a.audioCodecIsCopy ≠ b.audioCodecIsCopy then
        if a.audioCodecIsCopy then 1
        else if b.audioCodecIsCopy then -1
        else
          -- Step 4: audio sample rate
          if a.audioSampleRate > b.audioSampleRate then 1
          else if a.audioSampleRate < b.audioSampleRate then -1
          else 0
      else
        -- Step 4: audio sample rate
        if a.audioSampleRate > b.audioSampleRate then 1
        else if a.audioSampleRate < b.audioSampleRate then -1
        else 0

-- A simpler but equivalent formulation that factors out the shared tail:

/-- Auxiliary: compare the audio portion of two SenderKeys. -/
def SenderKey.compareAudio (a b : SenderKey) : Int :=
  if a.audioCodec ≠ b.audioCodec ∨ a.audioCodecIsCopy ≠ b.audioCodecIsCopy then
    if a.audioCodecIsCopy then 1
    else if b.audioCodecIsCopy then -1
    else
      if a.audioSampleRate > b.audioSampleRate then 1
      else if a.audioSampleRate < b.audioSampleRate then -1
      else 0
  else
    if a.audioSampleRate > b.audioSampleRate then 1
    else if a.audioSampleRate < b.audioSampleRate then -1
    else 0

/-- Simplified SenderKey comparison matching Go logic exactly.
    Models sender_key.go lines 45-77. -/
def SenderKey.cmp (a b : SenderKey) : Int :=
  -- Video codec check
  if a.videoCodecIsCopy ≠ b.videoCodecIsCopy ∨ a.videoCodec ≠ b.videoCodec then
    if a.videoCodecIsCopy then 1
    else if b.videoCodecIsCopy then -1
    else
      let resA := a.videoWidth * a.videoHeight
      let resB := b.videoWidth * b.videoHeight
      if resA > resB then 1
      else if resA < resB then -1
      else SenderKey.compareAudio a b
  else
    let resA := a.videoWidth * a.videoHeight
    let resB := b.videoWidth * b.videoHeight
    if resA > resB then 1
    else if resA < resB then -1
    else SenderKey.compareAudio a b

/-! ### 2. Codec Resource Reuse -/

/-- Parameters for codec resource reuse check.
    Mirrors canReuse from codec_resource_manager.go:69-108. -/
structure CodecReuseParams where
  paramsWidth     : Int
  paramsHeight    : Int
  paramsPixFmt    : Int   -- pixel format enum value; 0 = PixelFormatNone
  encoderWidth    : Int
  encoderHeight   : Int
  decoderPixFmt   : Int
  deriving Repr, DecidableEq

/-- canReuse: true iff width, height, and pixel format all match.
    When paramsPixFmt = 0 (None), pixel format check is skipped.
    Models codec_resource_manager.go:89-107. -/
def canReuse (p : CodecReuseParams) : Bool :=
  p.paramsWidth == p.encoderWidth &&
  p.paramsHeight == p.encoderHeight &&
  (p.paramsPixFmt == 0 || p.paramsPixFmt == p.decoderPixFmt)

/-! ### 3. Queue Size Estimation -/

/-- Estimate queue size on timeout for the active output.
    Models autobitrate_handler.go:436-438:
      nodeTotalQueue = previousQueueSize + uint64(tsDiff.Seconds()*float64(h.lastVideoBitRate)/8.0)

    All values are non-negative integers.
    timeDeltaNumer/timeDeltaDenom models tsDiff.Seconds() as a rational.
    bitRate is in bits/s; division by 8 converts to bytes/s. -/
def estimateQueueSize (prevQueue : Int) (timeDeltaNumer timeDeltaDenom : Int) (bitRate : Int) : Int :=
  prevQueue + timeDeltaNumer * bitRate / (8 * timeDeltaDenom)

/-- Simplified version when timeDelta is expressed in integer seconds. -/
def estimateQueueSizeSimple (prevQueue : Int) (timeDeltaSec : Int) (bitRate : Int) : Int :=
  prevQueue + timeDeltaSec * bitRate / 8

/-! ### 4. Latency Estimation -/

/-- Normal latency computation.
    Models stream_mux.go:1522-1529:
      if oldestDTS > 0: latency = earliestDTS - oldestDTS
      clamp to >= 0.
    All values in nanoseconds (Int). -/
def computeLatency (earliestDTS oldestDTS : Int) : Int :=
  if oldestDTS > 0 then max 0 (earliestDTS - oldestDTS)
  else 0

/-- Fallback latency on error.
    Models stream_mux.go:1589-1591:
      newValue = prevLatency + timeDelta
    (the Swap returns the old value, but the new stored value is prev+delta) -/
def fallbackLatency (prevLatencyNs : Int) (timeDeltaNs : Int) : Int :=
  prevLatencyNs + timeDeltaNs

/-! ### 5. AutoBitRateVideoConfig -/

/-- A resolution+bitrate config entry.
    Models AutoBitRateResolutionAndBitRateConfig from autobitrate_video_config.go:40-44. -/
structure ResConfig where
  width      : Int
  height     : Int
  bitrateHigh : Int
  bitrateLow  : Int
  deriving Repr, DecidableEq

/-- Pixel count for a config entry. -/
def ResConfig.pixels (c : ResConfig) : Int := c.width * c.height

/-- Find a config by exact resolution match.
    Models AutoBitRateResolutionAndBitRateConfigs.Find (autobitrate_video_config.go:52-61). -/
def findConfig (configs : List ResConfig) (w h : Int) : Option ResConfig :=
  configs.find? (fun c => c.width == w && c.height == h)

/-- Best config: the one with the largest pixel count.
    Models .Best() (autobitrate_video_config.go:75-86). -/
def bestConfig (configs : List ResConfig) : Option ResConfig :=
  configs.foldl (fun acc c =>
    match acc with
    | none => some c
    | some best => if c.pixels > best.pixels then some c else some best
  ) none

/-- Worst config: the one with the smallest pixel count.
    Models .Worst() (autobitrate_video_config.go:88-99). -/
def worstConfig (configs : List ResConfig) : Option ResConfig :=
  configs.foldl (fun acc c =>
    match acc with
    | none => some c
    | some worst => if c.pixels < worst.pixels then some c else some worst
  ) none

/-- A list of configs is sorted by ascending pixel count. -/
def sortedByPixels (configs : List ResConfig) : Prop :=
  configs.Pairwise (fun a b => a.pixels ≤ b.pixels)

end Validation
