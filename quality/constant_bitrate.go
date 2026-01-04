// constant_bitrate.go defines the constant bitrate type.

package quality

import (
	"encoding/json"
	"fmt"

	"github.com/asticode/go-astiav"
)

type ConstantBitrate uint

func (ConstantBitrate) typeName() string {
	return "constant_bitrate"
}

func (q ConstantBitrate) MarshalJSON() ([]byte, error) {
	return json.Marshal(qualitySerializable{
		"type":    q.typeName(),
		"bitrate": uint(q),
	})
}

func (q ConstantBitrate) Apply(cp *astiav.CodecParameters) error {
	cp.SetBitRate(int64(q))
	return nil
}

func (q *ConstantBitrate) setValues(in qualitySerializable) error {
	bitrate, ok := in["bitrate"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'bitrate' in %#+v", in)
	}

	*q = ConstantBitrate(bitrate)
	return nil
}
