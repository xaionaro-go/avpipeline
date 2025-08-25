package astiav

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

func DictionaryItemsToAstiav(s types.DictionaryItems) *astiav.Dictionary {
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
