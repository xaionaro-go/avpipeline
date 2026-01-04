// encoder_factory_option.go provides configuration options for encoder factories.

package codec

import (
	"github.com/xaionaro-go/avpipeline/codec/types"
)

type (
	Option        = types.Option
	Options       = types.Options
	OptionCommons = types.OptionCommons
)

func EncoderFactoryOptionLatest[T Option](s []Option) (ret T, ok bool) {
	return types.OptionLatest[T](s)
}

type EncoderFactoryOptionGetDecoderer struct {
	OptionCommons
	GetDecoderer
}

type EncoderFactoryOptionOnlyDummy struct {
	OptionCommons
	OnlyDummy bool
}
