// codec_custom_options.go provides low-latency option presets for various codecs.

package codec

import (
	"context"
	"strings"
)

func LowLatencyOptions(
	ctx context.Context,
	codecName Name,
	isEncoder bool,
) DictionaryItems {
	var result DictionaryItems
	if isEncoder {
		result = append(result, DictionaryItem{Key: "zerolatency", Value: "1"})
		result = append(result, DictionaryItem{Key: "bf", Value: "0"})
		result = append(result, DictionaryItem{Key: "forced-idr", Value: "1"}) // for quick recovery
		result = append(result, DictionaryItem{Key: "intra-refresh", Value: "0"})
		switch {
		case strings.HasSuffix(string(codecName), "_mediacodec"):
			result = append(result, DictionaryItem{Key: "priority", Value: "0"})
		case strings.HasSuffix(string(codecName), "_nvenc"):
			result = append(result, DictionaryItem{Key: "tune", Value: "ll"})
			result = append(result, DictionaryItem{Key: "delay", Value: "0"})
			result = append(result, DictionaryItem{Key: "rc-lookahead", Value: "0"})
			result = append(result, DictionaryItem{Key: "rc", Value: "cbr_ld_hq"})
		case codecName == "libx264":
			result = append(result, DictionaryItem{Key: "tune", Value: "zerolatency"})
		}
	}
	return result
}
