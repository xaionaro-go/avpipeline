package types

type OptionCommons struct{}

func (OptionCommons) codecOption() {}

type Option interface {
	codecOption()
}

type Options []Option

func OptionLatest[T Option](s Options) (ret T, ok bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if v, ok := s[i].(T); ok {
			return v, true
		}
	}
	return
}
