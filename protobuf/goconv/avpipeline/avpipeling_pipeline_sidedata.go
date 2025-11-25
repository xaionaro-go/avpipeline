package avpipeline

import (
	"encoding/json"

	"github.com/xaionaro-go/polyjson"
)

type PipelineSideData json.RawMessage

func PipelineSideDataFromProtobuf(input []byte) PipelineSideData {
	return (PipelineSideData)(input)
}

func PipelineSideDataFromGo(input any) PipelineSideData {
	if input == nil {
		return nil
	}
	b, err := polyjson.MarshalWithTypeIDs(input, polyjson.TypeRegistry())
	if err != nil {
		panic("unable to marshal pipeline side data: " + err.Error())
	}
	return b
}

func (f PipelineSideData) Protobuf() []byte {
	return f
}
