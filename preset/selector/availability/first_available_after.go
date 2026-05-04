package availability

import "context"

// FirstAvailableAfter returns the first available candidate after the supplied index.
func FirstAvailableAfter(
	ctx context.Context,
	members []Candidate,
	after int,
) (int, bool) {
	start := after + 1
	if start < 0 {
		start = 0
	}

	for idx := start; idx < len(members); idx++ {
		candidate := members[idx]
		if !candidate.Present {
			continue
		}
		if candidate.Source != nil && !candidate.Source.HasResources(ctx) {
			continue
		}
		return idx, true
	}

	return 0, false
}
