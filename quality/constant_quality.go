// constant_quality.go defines the constant quality type.

package quality

import (
	"encoding/json"
	"fmt"

	"github.com/asticode/go-astiav"
)

type ConstantQuality uint8

func (ConstantQuality) typeName() string {
	return "constant_quality"
}

func (q ConstantQuality) MarshalJSON() ([]byte, error) {
	return json.Marshal(qualitySerializable{
		"type":    q.typeName(),
		"quality": uint(q),
	})
}

func (q ConstantQuality) Apply(cp *astiav.CodecParameters) error {
	return fmt.Errorf("constant quality (%#+v) is not implemented, yet", q)
}

func (q *ConstantQuality) setValues(in qualitySerializable) error {
	bitrate, ok := in["quality"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'quality' in %#+v", in)
	}

	*q = ConstantQuality(bitrate)
	return nil
}
