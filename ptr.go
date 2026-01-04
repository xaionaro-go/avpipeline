// ptr.go provides a helper function for creating pointers to values.

package avpipeline

func ptr[T any](v T) *T {
	return &v
}
