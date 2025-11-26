package resource

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
		isEncoder bool,
		params *astiav.CodecParameters,
		timeBase astiav.Rational,
		opts ...types.Option,
	) *Resources
}
