package resourcegetter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
)

type ResourcesGetter interface {
	fmt.Stringer
	GetResources(
		ctx context.Context,
		params *astiav.CodecParameters,
		timeBase astiav.Rational,
		opts ...types.EncoderFactoryOption,
	) *Resources
}
