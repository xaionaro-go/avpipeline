-- Spec/StreamMux/BitrateControl.lean: Formal specification of three AutoBitRate
-- sub-components from preset/streammux/autobitrate_handler.go:
--   1. Resolution Selection  (getDesiredResolutionConfig + changeResolutionIfNeeded)
--   2. Bitrate Clamping / Slowdown (trySetVideoBitrate + checkSlowdown)
--   3. Temporary FPS Reduction (temporaryReduceFPS)
--
-- All arithmetic uses Int. Durations are Int milliseconds.

/-! # Resolution configuration -/

/-- A single resolution tier, mirroring `AutoBitRateResolutionAndBitRateConfig`. -/
structure ResConfig where
  width      : Int
  height     : Int
  bitrateLow : Int   -- lower bitrate threshold (bps)
  bitrateHigh : Int  -- upper bitrate threshold (bps)
  deriving Repr, DecidableEq

/-- Pixel count: width * height. -/
def ResConfig.pixels (r : ResConfig) : Int := r.width * r.height

/-- Resolution equality by dimensions. -/
def ResConfig.sameRes (a b : ResConfig) : Bool :=
  a.width == b.width && a.height == b.height

/-- A list of resolution tiers. -/
abbrev ResConfigs := List ResConfig

namespace ResConfigs

/-- Find a config matching the given resolution. -/
def find (cfgs : ResConfigs) (w h : Int) : Option ResConfig :=
  cfgs.find? (fun c => c.width == w && c.height == h)

/-- Filter to configs whose bitrate range covers the given bitrate. -/
def inRange (cfgs : ResConfigs) (bitrate : Int) : ResConfigs :=
  cfgs.filter (fun c => c.bitrateLow ≤ bitrate && bitrate ≤ c.bitrateHigh)

/-- Best (highest pixel count) config, or none. -/
def best : ResConfigs → Option ResConfig
  | [] => none
  | c :: cs => some (cs.foldl (fun acc x => if x.pixels > acc.pixels then x else acc) c)

/-- Worst (lowest pixel count) config, or none. -/
def worst : ResConfigs → Option ResConfig
  | [] => none
  | c :: cs => some (cs.foldl (fun acc x => if x.pixels < acc.pixels then x else acc) c)

end ResConfigs

/-! # 1. Resolution Selection -/

/-- Outcome of resolution selection. -/
inductive ResolutionAction where
  | noChange           -- bitrate is within current tier's [low, high]
  | switchTo (r : ResConfig)  -- switch to a different resolution
  | enableBypass       -- at max resolution and bitrate > high
  deriving Repr, DecidableEq

/--
  Models `getDesiredResolutionConfig`: given the allowed configs, the full
  config set, current resolution, and bitrate, pick the desired config.
  Returns `none` when no suitable config exists.
-/
def getDesiredResolutionConfig
    (allowed : ResConfigs)
    (all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int) : Option ResConfig :=
  if (allowed.find curW curH).isNone then
    -- Current resolution outside allowed set: pick best fitting, or best overall
    match allowed.inRange bitrate |>.best with
    | some c => some c
    | none   => allowed.best
  else
    match all.find curW curH with
    | none => none
    | some cur =>
      if bitrate < cur.bitrateLow then
        -- Below low threshold: pick best config in range, or worst allowed
        match allowed.inRange bitrate |>.best with
        | some c => some c
        | none   => allowed.worst
      else if bitrate > cur.bitrateHigh then
        -- Above high threshold: pick worst config in range, or best allowed
        match allowed.inRange bitrate |>.worst with
        | some c => some c
        | none   => allowed.best
      else
        -- Within range: stay at current
        some cur

/--
  Models `changeResolutionIfNeeded`: decides the action based on bitrate
  relative to the current resolution's thresholds.
  Parameters:
  - `allowed`: the allowed resolution configs
  - `all`: the full resolution config set
  - `curW`, `curH`: current encoder resolution
  - `bitrate`: the (clamped) bitrate
-/
def changeResolutionIfNeeded
    (allowed : ResConfigs)
    (all : ResConfigs)
    (curW curH : Int)
    (bitrate : Int) : ResolutionAction :=
  let currentAllowed := (allowed.find curW curH).isSome
  let desired := getDesiredResolutionConfig allowed all curW curH bitrate
  match all.find curW curH with
  | none => ResolutionAction.noChange  -- can't find config for current
  | some cur =>
    -- If outside allowed set, force switch
    if !currentAllowed then
      match desired with
      | some d => if d.sameRes cur then ResolutionAction.noChange
                  else ResolutionAction.switchTo d
      | none   => ResolutionAction.noChange
    else if bitrate >= cur.bitrateLow && bitrate <= cur.bitrateHigh then
      ResolutionAction.noChange
    else
      match desired with
      | some d => if d.sameRes cur then
                    -- Same as current but outside range: at boundary
                    if bitrate > cur.bitrateHigh then ResolutionAction.enableBypass
                    else ResolutionAction.noChange  -- at lowest
                  else ResolutionAction.switchTo d
      | none   =>
        if bitrate > cur.bitrateHigh then ResolutionAction.enableBypass
        else ResolutionAction.noChange

/-! # 2. Bitrate Clamping -/

/--
  Models the clamping logic in `trySetVideoBitrate`.
  Given `minBitRate`, `maxBitRate`, `inputBitRate`, and requested `bitrate`,
  compute the effective max and then clamp.
  `effectiveMax`: if inputBitRate > 2 * minBitRate and 3/2 * inputBitRate < maxBitRate,
  then effectiveMax = 3/2 * inputBitRate (in Int: 3 * inputBitRate / 2), else maxBitRate.
-/
def effectiveMaxBitRate (minBitRate maxBitRate inputBitRate : Int) : Int :=
  if inputBitRate > minBitRate * 2 && 3 * inputBitRate < 2 * maxBitRate then
    3 * inputBitRate / 2
  else
    maxBitRate

/-- Clamp a bitrate to [minBitRate, effectiveMax]. -/
def clampBitRate (minBitRate maxBitRate inputBitRate bitrate : Int) : Int :=
  let mx := effectiveMaxBitRate minBitRate maxBitRate inputBitRate
  if bitrate < minBitRate then minBitRate
  else if bitrate > mx then mx
  else bitrate

/--
  Models bypass activation check: bypass is enabled when
  `bitrate > 6/5 * inputBitRate`, i.e., `5 * bitrate > 6 * inputBitRate`.
-/
def shouldEnableBypass (bitrate inputBitRate : Int) : Bool :=
  5 * bitrate > 6 * inputBitRate

/--
  Models bypass deactivation check: bypass is disabled when
  `bitrate < inputBitRate`.
-/
def shouldDisableBypass (bitrate inputBitRate : Int) : Bool :=
  bitrate < inputBitRate

/-! # 3. Slowdown (rate-limiting state machine for resolution changes) -/

/-- State of a pending resolution change request. -/
structure SlowdownRequest where
  isUpgrade : Bool
  startedAt : Int   -- milliseconds timestamp
  latestAt  : Int   -- milliseconds timestamp
  deriving Repr, DecidableEq

/-- Outcome of the slowdown check. -/
inductive SlowdownResult where
  | notThisTime (newReq : SlowdownRequest)
  | proceed
  deriving Repr, DecidableEq

/--
  Models `checkSlowdown`.
  - `prev`: previous resolution change request (if any)
  - `isUpgrade`: whether this is an upgrade
  - `now`: current time in ms
  - `upgradeSlowdownMs`: base upgrade slowdown in ms
  - `downgradeSlowdownMs`: downgrade slowdown in ms (2000 in Go)
  - `targetPixels`, `avgPixels`: for scaling upgrade duration
-/
def checkSlowdownBody
    (p : SlowdownRequest)
    (isUpgrade : Bool)
    (now : Int)
    (upgradeSlowdownMs : Int)
    (downgradeSlowdownMs : Int)
    (targetPixels avgPixels : Int)
    : SlowdownResult :=
  let reqDur := now - p.startedAt
  let effectiveUpgradeMs :=
    if isUpgrade && targetPixels > avgPixels && avgPixels > 0 then
      upgradeSlowdownMs * targetPixels / avgPixels
    else
      upgradeSlowdownMs
  if isUpgrade then
    if reqDur < effectiveUpgradeMs then
      SlowdownResult.notThisTime ⟨isUpgrade, p.startedAt, now⟩
    else
      SlowdownResult.proceed
  else
    if reqDur < downgradeSlowdownMs then
      SlowdownResult.notThisTime ⟨isUpgrade, p.startedAt, now⟩
    else
      SlowdownResult.proceed

def checkSlowdown
    (prev : Option SlowdownRequest)
    (isUpgrade : Bool)
    (now : Int)
    (upgradeSlowdownMs : Int)
    (downgradeSlowdownMs : Int)
    (targetPixels avgPixels : Int)
    : SlowdownResult :=
  match prev with
  | none =>
    SlowdownResult.notThisTime ⟨isUpgrade, now, now⟩
  | some p =>
    if p.isUpgrade != isUpgrade || (now - p.latestAt > 60000) then
      SlowdownResult.notThisTime ⟨isUpgrade, now, now⟩
    else
      checkSlowdownBody p isUpgrade now upgradeSlowdownMs downgradeSlowdownMs targetPixels avgPixels

/-! # 4. Temporary FPS Reduction -/

/--
  State for the temporary FPS reduction mechanism.
  `multiplierNum / multiplierDen` is the current FPS reduction multiplier.
  `updatedAtMs` is the last update timestamp in ms.
-/
structure FPSReductionState where
  multiplierNum : Int
  multiplierDen : Int
  updatedAtMs   : Int
  deriving Repr, DecidableEq

/--
  Models `temporaryReduceFPS`.
  - `st`: current FPS reduction state
  - `bitrate`: requested bitrate
  - `bitrateBeyondThreshold`: negative value (how far below the low threshold)
  - `curEncodedBitRate`: current actual encoded bitrate
  - `nowMs`: current time in ms
  Returns `none` if rate-limited (< 1s since last update), else `some newState`.
-/
def temporaryReduceFPS
    (st : FPSReductionState)
    (bitrate : Int)
    (bitrateBeyondThreshold : Int)
    (curEncodedBitRate : Int)
    (nowMs : Int)
    : Option FPSReductionState :=
  if nowMs - st.updatedAtMs < 1000 then
    none  -- rate-limited
  else
    -- source0 = (prevNum/prevDen) * (bitrate/curEncodedBitRate)
    --         = prevNum * bitrate / (prevDen * curEncodedBitRate)
    -- source1 = bitrate / (bitrate - bitrateBeyondThreshold)
    -- avg_num/avg_den = (source0 + source1) / 2
    -- We represent as: newNum = source0_num * source1_den + source1_num * source0_den
    --                   newDen = 2 * source0_den * source1_den
    let s0Num := st.multiplierNum * bitrate
    let s0Den := st.multiplierDen * curEncodedBitRate
    let s1Num := bitrate
    let s1Den := bitrate - bitrateBeyondThreshold
    let newNum := s0Num * s1Den + s1Num * s0Den
    let newDen := 2 * s0Den * s1Den
    some ⟨newNum, newDen, nowMs⟩
