package availability

import "context"

// AlwaysAvailable is a Source that always reports resources.
type AlwaysAvailable struct{}

func (AlwaysAvailable) HasResources(context.Context) bool {
	return true
}
