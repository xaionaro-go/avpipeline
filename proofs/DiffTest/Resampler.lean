-- DiffTest/Resampler.lean: Differential test driver for Spec.Resampler
--
-- Protocol (line-based, space-separated):
--
--   expectedOutputSamples <inputSamples> <inRate> <outRate> <chunkSize>
--     Runs expectedOutputSamples from the spec.
--     Output: the computed sample count
--
--   fifo <write_count> <read_count>
--     Creates an empty FIFO, writes write_count sentinel values, reads read_count.
--     Output: <size_after_write> <items_read> <remaining_size>

import Spec.Resampler

namespace DiffTest.Resampler

private def processLine (line : String) : Except String String := do
  let parts := line.splitOn " " |>.filter (· ≠ "")
  match parts with
  | "expectedOutputSamples" :: rest =>
    match rest with
    | [inputSamplesStr, inRateStr, outRateStr, chunkSizeStr] =>
      let inputSamples ← inputSamplesStr.toNat?.elim (throw "invalid inputSamples") pure
      let inRate ← inRateStr.toNat?.elim (throw "invalid inRate") pure
      let outRate ← outRateStr.toNat?.elim (throw "invalid outRate") pure
      let chunkSize ← chunkSizeStr.toNat?.elim (throw "invalid chunkSize") pure
      let fmt : ResamplerFormat := ⟨outRate, chunkSize⟩
      let result := expectedOutputSamples fmt inRate inputSamples
      pure s!"{result}"
    | _ => throw "expectedOutputSamples: need <inputSamples> <inRate> <outRate> <chunkSize>"
  | "fifo" :: rest =>
    match rest with
    | [writeCountStr, readCountStr] =>
      let writeCount ← writeCountStr.toNat?.elim (throw "invalid write_count") pure
      let readCount ← readCountStr.toNat?.elim (throw "invalid read_count") pure
      -- Build a list of writeCount sentinel values (just natural numbers).
      let samples := List.range writeCount
      let fifo := AudioFifo.empty (α := Nat)
      let fifo := fifo.write samples
      let sizeAfterWrite := fifo.size
      let (taken, remaining) := fifo.read readCount
      let itemsRead := taken.length
      let remainingSize := remaining.size
      pure s!"{sizeAfterWrite} {itemsRead} {remainingSize}"
    | _ => throw "fifo: need <write_count> <read_count>"
  | _ => throw s!"unknown command: {parts.head?}"

def run : IO Unit := do
  let stdin ← IO.getStdin
  let mut line ← stdin.getLine
  while !line.isEmpty do
    let trimmed := line.trim
    if !trimmed.isEmpty then
      match processLine trimmed with
      | .ok result => IO.println result
      | .error msg => IO.eprintln s!"error: {msg}"
    line ← stdin.getLine

end DiffTest.Resampler
