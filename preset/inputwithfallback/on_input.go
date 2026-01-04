// on_input.go implements a condition that adds pipeline side data to inputs.

package inputwithfallback

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

func (i *InputChain[K, DF, C]) onInput() packetorframefiltercondition.Condition {
	return (*inputChainAsCondition[K, DF, C])(i)
}

type inputChainAsCondition[K InputKernel, DF codec.DecoderFactory, C any] InputChain[K, DF, C]

func (i *inputChainAsCondition[K, DF, C]) asInputChain() *InputChain[K, DF, C] {
	return (*InputChain[K, DF, C])(i)
}

func (i *inputChainAsCondition[K, DF, C]) String() string {
	return "InputWithFallbackCondition"
}

func (i *inputChainAsCondition[K, DF, C]) Match(
	_ context.Context,
	arg packetorframefiltercondition.Input,
) bool {
	arg.Input.AddPipelineSideData(i.asInputChain())
	return true
}
