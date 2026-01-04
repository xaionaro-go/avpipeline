// ptr.go provides a helper function to get a pointer to a value.

package quality

func ptr[T any](in T) *T {
	return &in
}
