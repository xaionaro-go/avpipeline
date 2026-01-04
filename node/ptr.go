// ptr.go provides a helper function for creating pointers to values.

package node

func ptr[T any](in T) *T {
	return &in
}
