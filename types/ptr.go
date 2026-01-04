// ptr.go provides a helper function for creating pointers to values.

package types

func ptr[T any](v T) *T {
	return &v
}
