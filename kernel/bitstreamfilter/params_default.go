package bitstreamfilter

import (
	"github.com/asticode/go-astiav"
)

func ParamsMP4ToMP2(codecID astiav.CodecID) []Params {
	switch codecID {
	case astiav.CodecIDH264:
		return []Params{{Name: NameH264MP4toAnnexB}}
	case astiav.CodecIDHevc:
		return []Params{{Name: NameHEVCMP4toAnnexB}}
		// TODO: add the case for 'NameVVCMP4toAnnexB'
	}
	return []Params{}
}

func ParamsMP2ToMP4(codecID astiav.CodecID) []Params {
	switch codecID {
	case astiav.CodecIDH264, astiav.CodecIDHevc:
		return []Params{
			{Name: NameExtractExtradata},
			//{Name: NameDumpExtra, Options: types.DictionaryItems{{Key: "freq", Value: "all"}}},
		}
	case astiav.CodecIDAac:
		return []Params{{Name: NameAACADTSToASC}}
	}
	return []Params{}
}

func ParamsMP4ToMP4(codecID astiav.CodecID) []Params {
	switch codecID {
	case astiav.CodecIDH264:
		return []Params{}
	}
	return []Params{}
}
