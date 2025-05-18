package autofix

import (
	"context"
	"io"

	"github.com/xaionaro-go/avpipeline"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/node"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ node.Abstract = (*AutoFixer[struct{}])(nil)

func (a *AutoFixer[T]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	var nodes []node.Abstract
	if a.AutoHeadersNode != nil {
		nodes = append(nodes, a.AutoHeadersNode)
	}
	if a.MapStreamIndicesNode != nil {
		nodes = append(nodes, a.MapStreamIndicesNode)
	}
	avpipeline.Serve(
		ctx,
		avpipeline.ServeConfig{
			EachNode: cfg,
		},
		errCh,
		nodes...,
	)
}

func (a *AutoFixer[T]) String() string {
	return "AutoFixer"
}

func (a *AutoFixer[T]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if writeToer, ok := any(a.Input()).(node.DotBlockContentStringWriteToer); ok {
		writeToer.DotBlockContentStringWriteTo(w, alreadyPrinted)
	}
}

func (a *AutoFixer[T]) GetPushPacketsTos() node.PushPacketsTos {
	return a.Output().GetPushPacketsTos()
}

func (a *AutoFixer[T]) AddPushPacketsTo(dst node.Abstract, conds ...packetcondition.Condition) {
	a.Output().AddPushPacketsTo(dst, conds...)
}

func (a *AutoFixer[T]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	a.Output().SetPushPacketsTos(pushTos)
}

func (a *AutoFixer[T]) GetPushFramesTos() node.PushFramesTos {
	return a.Output().GetPushFramesTos()
}

func (a *AutoFixer[T]) AddPushFramesTo(dst node.Abstract, conds ...framecondition.Condition) {
	a.Output().AddPushFramesTo(dst, conds...)
}

func (a *AutoFixer[T]) SetPushFramesTos(pushTos node.PushFramesTos) {
	a.Output().SetPushFramesTos(pushTos)
}

func (a *AutoFixer[T]) GetStatistics() *node.Statistics {
	inputStats := a.Input().GetStatistics().Convert()
	outputStats := a.Output().GetStatistics().Convert()
	return node.FromProcessingStatistics(&node.ProcessingStatistics{
		BytesCountRead:  inputStats.BytesCountRead,
		BytesCountWrote: outputStats.BytesCountWrote,
		FramesRead:      inputStats.FramesRead,
		FramesWrote:     outputStats.FramesWrote,
	})
}

func (a *AutoFixer[T]) GetProcessor() processor.Abstract {
	return a
}

func (a *AutoFixer[T]) GetInputPacketCondition() packetcondition.Condition {
	return a.Input().GetInputPacketCondition()
}

func (a *AutoFixer[T]) SetInputPacketCondition(cond packetcondition.Condition) {
	a.Input().SetInputPacketCondition(cond)
}

func (a *AutoFixer[T]) GetInputFrameCondition() framecondition.Condition {
	return a.Input().GetInputFrameCondition()
}

func (a *AutoFixer[T]) SetInputFrameCondition(cond framecondition.Condition) {
	a.Input().SetInputFrameCondition(cond)
}
