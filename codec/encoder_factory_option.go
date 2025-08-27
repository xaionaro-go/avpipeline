package codec

import (
	"github.com/xaionaro-go/avpipeline/codec/types"
)

type EncoderFactoryOption = types.EncoderFactoryOption
type EncoderFactoryOptions = types.EncoderFactoryOptions
type EncoderFactoryOptionCommons = types.EncoderFactoryOptionCommons

func EncoderFactoryOptionLatest[T EncoderFactoryOption](s []EncoderFactoryOption) (ret T, ok bool) {
	return types.EncoderFactoryOptionLatest[T](s)
}

type EncoderFactoryOptionGetDecoderer struct {
	EncoderFactoryOptionCommons
	GetDecoderer
}

type EncoderFactoryOptionOnlyDummy struct {
	EncoderFactoryOptionCommons
	OnlyDummy bool
}

type EncoderFactoryOptionReusableResources struct {
	EncoderFactoryOptionCommons
	*Resources
}
