// bitrates.go defines structures for tracking bitrates of different stream types.

package types

import globaltypes "github.com/xaionaro-go/avpipeline/types"

type BitRateInfo = globaltypes.BitRateInfo

type BitRates struct {
	Input   BitRateInfo
	Encoded BitRateInfo
	Output  BitRateInfo
}
