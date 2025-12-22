package inputwithfallback

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

func (i *InputChain[K, DF, C]) onInputFrame() framefiltercondition.Condition {
	return (*inputChainAsFrameCondition[K, DF, C])(i)
}

type inputChainAsFrameCondition[K InputKernel, DF codec.DecoderFactory, C any] InputChain[K, DF, C]

func (i *inputChainAsFrameCondition[K, DF, C]) asInputChain() *InputChain[K, DF, C] {
	return (*InputChain[K, DF, C])(i)
}

func (i *inputChainAsFrameCondition[K, DF, C]) String() string {
	return "InputWithFallbackFrameCondition"
}

func (i *inputChainAsFrameCondition[K, DF, C]) Match(
	_ context.Context,
	arg framefiltercondition.Input,
) bool {
	arg.Input.AddPipelineSideData(i.asInputChain())
	return true
}

func (i *InputChain[K, DF, C]) onInputPacket() packetfiltercondition.Condition {
	return (*inputChainAsPacketCondition[K, DF, C])(i)
}

type inputChainAsPacketCondition[K InputKernel, DF codec.DecoderFactory, C any] InputChain[K, DF, C]

func (i *inputChainAsPacketCondition[K, DF, C]) asInputChain() *InputChain[K, DF, C] {
	return (*InputChain[K, DF, C])(i)
}

func (i *inputChainAsPacketCondition[K, DF, C]) String() string {
	return "InputWithFallbackPacketCondition"
}

func (i *inputChainAsPacketCondition[K, DF, C]) Match(
	_ context.Context,
	arg packetfiltercondition.Input,
) bool {
	arg.Input.AddPipelineSideData(i.asInputChain())
	return true
}
