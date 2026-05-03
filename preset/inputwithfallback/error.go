// error.go defines custom error types used within the inputwithfallback package.

package inputwithfallback

import "fmt"

// ErrCannotPauseSoleActiveChain is returned by PauseChain when the
// requested chain is the only unpaused chain. Pausing it would leave
// zero active inputs, stopping the pipeline entirely.
type ErrCannotPauseSoleActiveChain struct {
	ID InputID
}

func (e ErrCannotPauseSoleActiveChain) Error() string {
	return fmt.Sprintf("cannot pause input chain %d: it is the sole active chain", e.ID)
}

// ErrSwitchInProgress is returned by InputSwitch's OnSwitchRequest gate
// when another switch is already in flight (switchingProcN > 0). It is
// used by callers (notably onInputChainError) to distinguish the
// by-design startup-walk contention across consecutive empty fallback
// slots from genuine concurrent-switch contention; QuietOnOpenFailure
// gates downgrading the former to Debug.
type ErrSwitchInProgress struct {
	ProcN int64
	To    int32
}

func (e ErrSwitchInProgress) Error() string {
	return fmt.Sprintf("another switch is in progress (procN: %d), cannot switch to %d", e.ProcN, e.To)
}

// Is returns true for any other ErrSwitchInProgress value, so callers
// can sentinel-match the type without caring about the populated
// ProcN/To fields. The struct fields are diagnostic-only.
func (e ErrSwitchInProgress) Is(target error) bool {
	_, ok := target.(ErrSwitchInProgress)
	return ok
}
