import DiffTest

/--
  Differential test dispatcher.
  Usage: difftest <component>
  Components: condition, drain, graph
  Reads test vectors from stdin, writes results to stdout.
-/
def main (args : List String) : IO Unit := do
  match args with
  | ["condition"] => DiffTest.Condition.main
  | ["drain"] => DiffTest.Drain.main
  | ["graph"] => DiffTest.Graph.main
  | ["quality"] => DiffTest.Quality.run
  | ["reorderdts"] => DiffTest.ReorderDTS.run
  | ["monotonic"] => DiffTest.MonotonicPTS.main
  | ["framerate"] => DiffTest.LimitFramerate.main
  | ["bitrate"] => DiffTest.LimitBitrate.main
  | ["reduceframerate"] => DiffTest.ReduceFramerate.run
  | ["router"] => DiffTest.Router.run
  | ["resampler"] => DiffTest.Resampler.run
  | ["rational"] => DiffTest.Rational.main
  | ["units"] => DiffTest.Units.main
  | ["statistics"] => DiffTest.Statistics.main
  | _ =>
    IO.eprintln "Usage: difftest <component>"
    IO.eprintln "Components: condition, drain, graph, quality, reorderdts, monotonic, framerate, bitrate, reduceframerate, router, resampler, rational, units, statistics"
    IO.Process.exit 1
