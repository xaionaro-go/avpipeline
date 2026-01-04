// type_alias.go defines package-level type aliases for common types.

package codec

import (
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/avpipeline/types"
)

type (
	Closer             = types.Closer
	HardwareDeviceType = types.HardwareDeviceType
	HardwareDeviceName = types.HardwareDeviceName
	DictionaryItem     = types.DictionaryItem
	DictionaryItems    = types.DictionaryItems
	InputPacket        = packet.Input
	Quality            = quality.Quality
)
