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
