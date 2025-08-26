package astiav

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/types"
)

func DictionaryItemsToAstiav(
	ctx context.Context,
	s types.DictionaryItems,
) *astiav.Dictionary {
	if s == nil {
		return nil
	}

	result := astiav.NewDictionary()
	setFinalizerFree(ctx, result)
	for _, opt := range s {
		logger.Tracef(ctx, "setting custom option: %s=%s", opt.Key, opt.Value)
		result.Set(opt.Key, opt.Value, 0)
	}
	return result
}
