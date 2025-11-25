package libav

func ptr[T any](v T) *T {
	return &v
}
