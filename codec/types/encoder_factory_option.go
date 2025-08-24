package types

type EncoderFactoryOptionCommons struct{}

func (EncoderFactoryOptionCommons) encoderFactoryOption() {}

type EncoderFactoryOption interface {
	encoderFactoryOption()
}

type EncoderFactoryOptions []EncoderFactoryOption

func EncoderFactoryOptionLatest[T EncoderFactoryOption](s EncoderFactoryOptions) (ret T, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if v, ok := s[i].(T); ok {
			return v, true
		}
	}
	return
}
