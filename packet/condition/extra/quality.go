package extra

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/quality"
)

type QualityT struct {
	Measurements *quality.Measurements
}

var _ condition.Condition = (*QualityT)(nil)

func NewQuality() *QualityT {
	return &QualityT{
		Measurements: quality.NewMeasurements(),
	}
}

func New() *QualityT {
	return &QualityT{}
}

func (f *QualityT) String() string {
	return "Quality"
}

func (f *QualityT) Match(
	ctx context.Context,
	in packet.Input,
) bool {
	f.Measurements.ObservePacketOrFrame(ctx, packetorframe.InputUnion{
		Packet: &in,
	})
	return true
}
