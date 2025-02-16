package types

import "github.com/asticode/go-astiav"

type HardwareDeviceType = astiav.HardwareDeviceType
type HardwareDeviceName string
type DictionaryItem struct {
	Key   string
	Value string
}
type DictionaryItems []DictionaryItem
