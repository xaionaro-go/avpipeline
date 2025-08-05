package types

import (
	"context"

	"github.com/asticode/go-astiav"
)

type HardwareDeviceType = astiav.HardwareDeviceType
type HardwareDeviceName string
type DictionaryItem struct {
	Key   string
	Value string
}
type DictionaryItems []DictionaryItem

func (s DictionaryItems) ToAstiav() *astiav.Dictionary {
	if s == nil {
		return nil
	}

	result := astiav.NewDictionary()
	setFinalizerFree(context.TODO(), result)
	for _, opt := range s {
		result.Set(opt.Key, opt.Value, 0)
	}
	return result
}
