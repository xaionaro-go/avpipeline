//go:build with_libav
// +build with_libav

package monitor

func ptr[T any](v T) *T {
	return &v
}
