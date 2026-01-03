// type_alias.go defines package-level type aliases for common types.

package codec

import (
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/avpipeline/types"
)

type Closer = types.Closer
type HardwareDeviceType = types.HardwareDeviceType
type HardwareDeviceName = types.HardwareDeviceName
type DictionaryItem = types.DictionaryItem
type DictionaryItems = types.DictionaryItems
type InputPacket = packet.Input
type Quality = quality.Quality
