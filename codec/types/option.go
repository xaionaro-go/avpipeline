// option.go defines the Option interface for codecs.

package types

import (
	"github.com/xaionaro-go/avpipeline/types"
)

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

type OptionOverrideHardwareDeviceType types.HardwareDeviceType

func (OptionOverrideHardwareDeviceType) codecOption() {}

type OptionOverrideCustomOptions types.DictionaryItems

func (OptionOverrideCustomOptions) codecOption() {}
