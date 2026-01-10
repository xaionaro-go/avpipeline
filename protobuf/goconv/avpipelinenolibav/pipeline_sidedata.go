// pipeline_sidedata.go provides conversion functions for pipeline side data between Protobuf and Go.

package avpipelinenolibav

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/polyjson"
)

type PipelineSideData json.RawMessage

func PipelineSideDataFromProtobuf(input []byte) PipelineSideData {
	return (PipelineSideData)(input)
}

func PipelineSideDataFromGo(input any) (ret PipelineSideData, _err error) {
	if input == nil {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("panic during marshaling pipeline side data: %v", r)
		}
	}()
	b, err := polyjson.MarshalWithTypeIDs(input, polyjson.TypeRegistry())
	if err != nil {
		return nil, fmt.Errorf("unable to marshal pipeline side data: %w", err)
	}
	return b, nil
}

func (f PipelineSideData) Protobuf() []byte {
	return f
}
