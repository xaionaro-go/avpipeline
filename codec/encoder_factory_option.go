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

// EncoderFactoryOptionLatest returns the last option of type T from s. Custom
// EncoderFactory implementations use it to read optional context passed by
// kernel.Encoder, for example EncoderFactoryOptionGetDecoderer.
func EncoderFactoryOptionLatest[T Option](s []Option) (ret T, ok bool) {
	return types.OptionLatest[T](s)
}

// EncoderFactoryOptionGetDecoderer carries access to the decoder that produced
// the frames being encoded. The option is present when the frame source
// implements GetDecoderer; factories that only need stream metadata should use
// the NewEncoder params and timeBase arguments instead.
type EncoderFactoryOptionGetDecoderer struct {
	OptionCommons
	GetDecoderer
}

// EncoderFactoryOptionOnlyDummy asks a factory to create only dummy encoders.
// kernel.Encoder uses this while initializing streams from packet-source
// metadata before decoded frames are available.
type EncoderFactoryOptionOnlyDummy struct {
	OptionCommons
	OnlyDummy bool
}
