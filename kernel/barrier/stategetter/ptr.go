// ptr.go provides a utility function to get a pointer to a value.
package stategetter

func ptr[T any](v T) *T {
	return &v
}
