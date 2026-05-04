package orphanretry

import "time"

// ExponentialConfig configures an exponential recreate retry policy.
type ExponentialConfig struct {
	// InitialDelay is required and schedules the next retry after the first failed tick attempt.
	InitialDelay time.Duration
	// MaxDelay caps exponential delay growth; zero leaves the delay uncapped.
	MaxDelay time.Duration
	// Multiplier grows the delay after each failed tick attempt and must be at least one.
	Multiplier float64
	// MaxAttempts retires an orphan after a finite tick-attempt budget; zero retries indefinitely, and finite budgets must be at least two.
	MaxAttempts uint64
	// MaxAge retires an orphan after a finite orphan era age; zero disables age retirement.
	MaxAge time.Duration
}
