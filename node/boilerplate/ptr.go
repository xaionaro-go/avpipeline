// ptr.go provides a helper function for creating pointers to values.

package boilerplate

func ptr[T any](v T) *T {
	return &v
}
