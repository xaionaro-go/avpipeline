package orphanretry

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

var (
	errInitialDelayRequired = errors.New("initial delay must be greater than zero")
	errInvalidMaxDelay      = errors.New("max delay must be zero or greater than or equal to initial delay")
	errInvalidMultiplier    = errors.New("multiplier must be finite and at least one")
	errInvalidMaxAttempts   = errors.New("max attempts must be zero for unlimited or at least two")
	errInvalidMaxAge        = errors.New("max age must be zero or greater")
)

// ExponentialPolicy validates cfg and returns an exponential recreate retry policy.
func ExponentialPolicy[K comparable](
	cfg ExponentialConfig,
) (Policy[K], error) {
	if err := validateExponentialConfig(cfg); err != nil {
		return nil, selectorerr.InvalidConfig("ExponentialPolicy", err)
	}

	return exponentialPolicy[K]{cfg: cfg}, nil
}

type exponentialPolicy[K comparable] struct {
	cfg ExponentialConfig
}

func (p exponentialPolicy[K]) Next(
	now time.Time,
	state State[K],
) Decision {
	if p.shouldRetire(now, state) {
		return Decision{Retire: true}
	}
	if !now.Before(state.NextAttemptAt) {
		return Decision{
			Attempt: true,
			NextAt:  now.Add(p.delayAfterAttempt(state.Attempts)),
		}
	}

	return Decision{NextAt: state.NextAttemptAt}
}

func (p exponentialPolicy[K]) shouldRetire(
	now time.Time,
	state State[K],
) bool {
	switch {
	case p.cfg.MaxAttempts > 0 && state.Attempts >= p.cfg.MaxAttempts:
		return true
	case p.cfg.MaxAge > 0 && !now.Before(state.FirstAttemptAt.Add(p.cfg.MaxAge)):
		return true
	default:
		return false
	}
}

func (p exponentialPolicy[K]) delayAfterAttempt(
	attempts uint64,
) time.Duration {
	if p.cfg.Multiplier == 1 {
		return p.cfg.InitialDelay
	}

	delay := float64(p.cfg.InitialDelay) * math.Pow(p.cfg.Multiplier, float64(attempts))
	if p.cfg.MaxDelay > 0 && delay > float64(p.cfg.MaxDelay) {
		return p.cfg.MaxDelay
	}
	if delay > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}

	return time.Duration(delay)
}

func validateExponentialConfig(
	cfg ExponentialConfig,
) error {
	var errs []error

	switch {
	case cfg.InitialDelay <= 0:
		errs = append(errs, errInitialDelayRequired)
	}
	switch {
	case cfg.MaxDelay < 0:
		errs = append(errs, errInvalidMaxDelay)
	case cfg.MaxDelay > 0 && cfg.MaxDelay < cfg.InitialDelay:
		errs = append(errs, fmt.Errorf("%w: %s < %s", errInvalidMaxDelay, cfg.MaxDelay, cfg.InitialDelay))
	}
	switch {
	case math.IsNaN(cfg.Multiplier):
		errs = append(errs, errInvalidMultiplier)
	case math.IsInf(cfg.Multiplier, 0):
		errs = append(errs, errInvalidMultiplier)
	case cfg.Multiplier < 1:
		errs = append(errs, errInvalidMultiplier)
	}
	switch {
	case cfg.MaxAttempts == 1:
		errs = append(errs, errInvalidMaxAttempts)
	}
	switch {
	case cfg.MaxAge < 0:
		errs = append(errs, errInvalidMaxAge)
	}

	return errors.Join(errs...)
}
