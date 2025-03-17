package quality

import (
	"encoding/json"
	"fmt"
)

type ConstantBitrate uint

func (ConstantBitrate) typeName() string {
	return "constant_bitrate"
}

func (vq ConstantBitrate) MarshalJSON() ([]byte, error) {
	return json.Marshal(qualitySerializable{
		"type":    vq.typeName(),
		"bitrate": uint(vq),
	})
}

func (vq *ConstantBitrate) setValues(in qualitySerializable) error {
	bitrate, ok := in["bitrate"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'bitrate' in %#+v", in)
	}

	*vq = ConstantBitrate(bitrate)
	return nil
}
