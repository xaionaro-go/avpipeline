package streammux

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}
