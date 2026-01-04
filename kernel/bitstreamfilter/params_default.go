// params_default.go provides default bitstream filter parameters.

package bitstreamfilter

import (
	"github.com/asticode/go-astiav"
)

func ParamsMP4ToMP2(codecID astiav.CodecID) []Params {
	return []Params{}
}

func ParamsMP2ToMP4(codecID astiav.CodecID) []Params {
	switch codecID {
	case astiav.CodecIDAv1, astiav.CodecIDH264, astiav.CodecIDHevc:
		return []Params{
			{Name: NameExtractExtradata},
		}
	case astiav.CodecIDAac:
		return []Params{{Name: NameAACADTSToASC}}
	}
	return []Params{}
}

func ParamsMP4ToMP4(codecID astiav.CodecID) []Params {
	return []Params{}
}
