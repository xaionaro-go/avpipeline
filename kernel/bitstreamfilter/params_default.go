package bitstreamfilter

import (
	"runtime"

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
		}
	case astiav.CodecIDAac:
		return []Params{{Name: NameAACADTSToASC}}
	}
	return []Params{}
}

func ParamsMP4ToMP4(codecID astiav.CodecID) []Params {
	switch codecID {
	case astiav.CodecIDH264:
		if runtime.GOARCH == "arm64" {
			// TODO: investigate why is this required. For some reason libav's h264_mediacodec inserts the extract_extradata without anybody asking for that and failing, so we convert to AnnexB to satisfy the extract_extradata. The case when it happened is: ffstream with passthrough of rtmp to rtmp.
			return []Params{{Name: NameH264MP4toAnnexB}}
		}
	}
	return []Params{}
}
