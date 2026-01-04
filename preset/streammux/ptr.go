// ptr.go provides a helper function to get a pointer to a value.

package streammux

func ptr[T any](v T) *T {
	return &v
}
