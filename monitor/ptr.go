//go:build with_libav
// +build with_libav

// ptr.go provides a helper function for creating pointers to values.

package monitor

func ptr[T any](v T) *T {
	return &v
}
