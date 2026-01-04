// ptr.go provides a generic helper function to create a pointer to a value.

package kernel

func ptr[T any](in T) *T {
	return &in
}
