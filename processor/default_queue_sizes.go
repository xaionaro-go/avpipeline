// default_queue_sizes.go centralizes the package-level default queue sizes
// consumed by DefaultOptionsTranscoder and DefaultOptionsOutput, and
// exposes a validated typed setter (SetDefaultQueueSizes) so callers can
// override them without touching package-level vars directly.

package processor

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// MaxQueueSize is the upper bound accepted by SetDefaultQueueSizes for any
// per-role queue capacity. Values above this are rejected as suspiciously
// large; the worst plausible production buffering is well under this cap.
const MaxQueueSize uint64 = 100000

// QueueSizes bundles the per-channel queue capacities applied to a single
// FromKernel processor.
type QueueSizes struct {
	Input  uint
	Output uint
	Error  uint
}

// DefaultQueueSizes is the package-level default queue size configuration
// returned by the transcoder/output factories.
type DefaultQueueSizes struct {
	Transcoder QueueSizes
	Output     QueueSizes
}

// builtinDefaultQueueSizes captures the compiled-in defaults. These are
// the values returned by DefaultOptionsTranscoder/DefaultOptionsOutput
// when SetDefaultQueueSizes has never been called or when a particular
// role/channel was passed 0 (sentinel = leave unchanged).
var builtinDefaultQueueSizes = DefaultQueueSizes{
	Transcoder: QueueSizes{Input: 60, Output: 10, Error: 2},
	Output:     QueueSizes{Input: 60, Output: 0, Error: 2},
}

// defaultQueueSizes holds the live default queue size configuration.
// Read via loadDefaultQueueSizes; written via SetDefaultQueueSizes.
// Stored behind atomic.Pointer so concurrent factory calls observe a
// consistent snapshot regardless of when SetDefaultQueueSizes runs.
var defaultQueueSizes atomic.Pointer[DefaultQueueSizes]

func loadDefaultQueueSizes() DefaultQueueSizes {
	if v := defaultQueueSizes.Load(); v != nil {
		return *v
	}
	return builtinDefaultQueueSizes
}

// SetDefaultQueueSizes overrides the package-level default queue sizes
// consumed by DefaultOptionsTranscoder and DefaultOptionsOutput.
//
// Each parameter is interpreted with sentinel-zero semantics: a value of 0
// leaves the corresponding role/channel at its current setting. To set a
// queue capacity to literal zero, callers must use the per-call Option
// overrides (OptionQueueSize{Input,Output,Error}) on the specific
// constructor.
//
// Validation:
//   - Each parameter must be <= MaxQueueSize. Larger values are rejected.
//   - On error, the package-level state is left unchanged.
//
// Subsequent calls to NewTranscoder, NewOutputFromURL, or any caller of
// DefaultOptionsTranscoder/DefaultOptionsOutput will observe the new
// values. Already-constructed processors are unaffected.
func SetDefaultQueueSizes(
	transcoderInput uint64,
	transcoderOutput uint64,
	transcoderError uint64,
	outputInput uint64,
	outputOutput uint64,
	outputError uint64,
) error {
	values := []struct {
		name  string
		value uint64
	}{
		{"transcoderInput", transcoderInput},
		{"transcoderOutput", transcoderOutput},
		{"transcoderError", transcoderError},
		{"outputInput", outputInput},
		{"outputOutput", outputOutput},
		{"outputError", outputError},
	}
	var errs []error
	for _, v := range values {
		if v.value > MaxQueueSize {
			errs = append(errs, fmt.Errorf("%s=%d exceeds MaxQueueSize=%d", v.name, v.value, MaxQueueSize))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	current := loadDefaultQueueSizes()
	next := current
	if transcoderInput != 0 {
		next.Transcoder.Input = uint(transcoderInput)
	}
	if transcoderOutput != 0 {
		next.Transcoder.Output = uint(transcoderOutput)
	}
	if transcoderError != 0 {
		next.Transcoder.Error = uint(transcoderError)
	}
	if outputInput != 0 {
		next.Output.Input = uint(outputInput)
	}
	if outputOutput != 0 {
		next.Output.Output = uint(outputOutput)
	}
	if outputError != 0 {
		next.Output.Error = uint(outputError)
	}
	defaultQueueSizes.Store(&next)
	return nil
}
