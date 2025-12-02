package avpipeline

import (
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipelinenolibav"
)

type PipelineSideData = avpipelinenolibav.PipelineSideData

func PipelineSideDataFromProtobuf(input []byte) PipelineSideData {
	return avpipelinenolibav.PipelineSideDataFromProtobuf(input)
}

func PipelineSideDataFromGo(input any) PipelineSideData {
	return avpipelinenolibav.PipelineSideDataFromGo(input)
}
