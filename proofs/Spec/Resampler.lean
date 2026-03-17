-- Spec/Resampler.lean: Formal specification of audio resampler from resampler/resampler.go

/-!
  Models the core algorithmic logic of the audio resampler:
  - expectedOutputSamples: compute output sample count from input
  - FIFO buffer: list-based model with capacity and read threshold
  - Format change detection
-/

/-! ## Ceiling division -/

/-- Ceiling division: ⌈a / b⌉ for positive b. Returns 0 when b = 0. -/
def ceilDiv (a b : Nat) : Nat :=
  if b = 0 then 0
  else (a + b - 1) / b

/-! ## expectedOutputSamples -/

/-- Resampler output format parameters. -/
structure ResamplerFormat where
  sampleRate : Nat
  chunkSize  : Nat
  deriving Repr, DecidableEq

/--
  Mirrors `expectedOutputSamples` from resampler.go lines 213-229.

  Go logic:
  - If inputSamples <= 0, return chunkSize
  - inRate = outputRate (fallback) or inputRate if set and > 0
  - If inRate == 0, return inputSamples
  - outSamples = ceil(inputSamples * outputRate / inRate)
  - outSamples = max(outSamples, chunkSize)
  - return outSamples + chunkSize
-/
def expectedOutputSamples (fmt : ResamplerFormat) (inRate : Nat) (inputSamples : Nat) : Nat :=
  let outSamples := ceilDiv (inputSamples * fmt.sampleRate) inRate
  let outSamples := max outSamples fmt.chunkSize
  outSamples + fmt.chunkSize

/-! ## FIFO buffer model -/

/-- Audio FIFO buffer modeled as a list of samples with a read threshold. -/
structure AudioFifo (α : Type) where
  data     : List α
  deriving Repr

namespace AudioFifo

def empty : AudioFifo α := ⟨[]⟩

def size (fifo : AudioFifo α) : Nat := fifo.data.length

/-- Write samples to the FIFO (append to end). -/
def write (fifo : AudioFifo α) (samples : List α) : AudioFifo α :=
  ⟨fifo.data ++ samples⟩

/-- Read up to `n` samples from the FIFO (take from front). -/
def read (fifo : AudioFifo α) (n : Nat) : List α × AudioFifo α :=
  let taken := fifo.data.take n
  let remaining := fifo.data.drop n
  (taken, ⟨remaining⟩)

/--
  Mirrors `receiveFrameLocked` from resampler.go lines 172-197.

  Returns:
  - `.eof` when FIFO is empty
  - `.eagain` when FIFO has data but less than minSize
  - `.ok samples` when enough data available
-/
inductive ReadResult (α : Type) where
  | eof
  | eagain
  | ok (samples : List α) (remaining : AudioFifo α)
  deriving Repr

def readWithThreshold (fifo : AudioFifo α) (minSize : Nat) (readCount : Nat) :
    ReadResult α :=
  if fifo.size = 0 then .eof
  else if fifo.size < minSize then .eagain
  else
    let (taken, rest) := fifo.read readCount
    .ok taken rest

end AudioFifo

/-! ## Format change detection -/

/-- Audio PCM format (subset relevant to resampling). -/
structure PCMFormat where
  sampleFormat  : Nat  -- opaque ID
  sampleRate    : Nat
  channelLayout : Nat  -- opaque ID
  deriving Repr, DecidableEq

/-- Result of format consistency check in sendFrameLocked. -/
inductive FormatCheckResult where
  | ok
  | formatChanged
  deriving Repr, DecidableEq

/--
  Mirrors the format check in sendFrameLocked (lines 128-134).
  If a previous format was recorded and differs from the new one, return error.
-/
def checkFormat (prev : Option PCMFormat) (curr : PCMFormat) : FormatCheckResult :=
  match prev with
  | none => .ok
  | some p => if p == curr then .ok else .formatChanged
