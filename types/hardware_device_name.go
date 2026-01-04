// hardware_device_name.go defines the HardwareDeviceName type.

package types

import (
	"encoding/json"
)

type HardwareDeviceName string

func (n *HardwareDeviceName) UnmarshalYAML(b []byte) error {
	return json.Unmarshal(b, (*string)(n))
}

func (n HardwareDeviceName) MarshalYAML() ([]byte, error) {
	return json.Marshal(string(n))
}
