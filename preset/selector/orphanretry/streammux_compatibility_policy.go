package orphanretry

import "time"

// StreamMuxCompatibilityPolicy returns the legacy streammux recreate retry policy.
func StreamMuxCompatibilityPolicy[K comparable]() Policy[K] {
	return streamMuxCompatibilityPolicy[K]{}
}

type streamMuxCompatibilityPolicy[K comparable] struct{}

func (streamMuxCompatibilityPolicy[K]) Next(
	now time.Time,
	_ State[K],
) Decision {
	return Decision{
		Attempt: true,
		NextAt:  now,
	}
}
