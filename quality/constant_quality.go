package quality

import (
	"encoding/json"
	"fmt"
)

type ConstantQuality uint8

func (ConstantQuality) typeName() string {
	return "constant_quality"
}

func (vq ConstantQuality) MarshalJSON() ([]byte, error) {
	return json.Marshal(qualitySerializable{
		"type":    vq.typeName(),
		"quality": uint(vq),
	})
}

func (vq *ConstantQuality) setValues(in qualitySerializable) error {
	bitrate, ok := in["quality"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'quality' in %#+v", in)
	}

	*vq = ConstantQuality(bitrate)
	return nil
}
