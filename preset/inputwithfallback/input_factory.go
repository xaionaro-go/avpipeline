package inputwithfallback

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
)

type InputFactory[K kernel.Abstract, DF codec.DecoderFactory] interface {
	fmt.Stringer
	NewInput(
		ctx context.Context,
	) (K, error)
	NewDecoderFactory(
		ctx context.Context,
	) (DF, error)
}
