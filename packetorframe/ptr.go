// ptr.go provides a helper function to get a pointer to a value.

package packetorframe

func ptr[T any](v T) *T {
	return &v
}
