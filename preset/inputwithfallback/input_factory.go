package inputwithfallback

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
)

type InputFactory[K InputKernel, DF codec.DecoderFactory, C any] interface {
	fmt.Stringer
	NewInput(
		ctx context.Context,
		chain *InputChain[K, DF, C],
	) (K, error)
	NewDecoderFactory(
		ctx context.Context,
		chain *InputChain[K, DF, C],
	) (DF, error)
}
