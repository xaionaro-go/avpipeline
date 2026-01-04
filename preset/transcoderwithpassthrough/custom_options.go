// custom_options.go provides helpers to convert custom options for the transcoder.

package transcoderwithpassthrough

import (
	"github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	avptypes "github.com/xaionaro-go/avpipeline/types"
)

func convertCustomOptions(
	opts types.DictionaryItems,
) avptypes.DictionaryItems {
	r := make(avptypes.DictionaryItems, 0, len(opts))
	for _, v := range opts {
		r = append(r, avptypes.DictionaryItem{
			Key:   v.Key,
			Value: v.Value,
		})
	}
	return r
}
