package codec

import (
	"slices"

	"github.com/asticode/go-astiav"
)

func (c *Codec) GetMIMEType() []string {
	result := c.getIANAMIMETypes()
	if androidMIMEType := c.GetAndroidMIMEType(); androidMIMEType != "" {
		if !slices.Contains(result, androidMIMEType) {
			return append(result, androidMIMEType)
		}
	}
	return result
}

func (c *Codec) getIANAMIMETypes() []string {
	switch c.CodecContext().CodecID() {
	case astiav.CodecIDH264:
		return []string{"video/H264"}
	case astiav.CodecIDHevc:
		return []string{"video/H265", "video/HEVC"}
	case astiav.CodecIDAv1:
		return []string{"video/AV1"}
	case astiav.CodecIDMpeg4:
		return []string{"video/mp4v-es"}
	case astiav.CodecIDMpeg2Video:
		return []string{"video/mpeg"}
	case astiav.CodecIDVp8:
		return []string{"video/VP8"}
	case astiav.CodecIDVp9:
		return []string{"video/VP9"}
	case astiav.CodecIDH263:
		return []string{"video/H263"}
	case astiav.CodecIDAac, astiav.CodecIDAacLatm:
		return []string{"audio/mp4a-latm"}
	case astiav.CodecIDMp2, astiav.CodecIDMp3:
		return []string{"audio/mpeg"}
	case astiav.CodecIDOpus:
		return []string{"audio/opus"}
	case astiav.CodecIDVorbis:
		return []string{"audio/vorbis"}
	case astiav.CodecIDFlac:
		return []string{"audio/flac"}
	case astiav.CodecIDAc3:
		return []string{"audio/ac3"}
	case astiav.CodecIDPcmMulaw:
		return []string{"audio/PCMU"}
	case astiav.CodecIDPcmAlaw:
		return []string{"audio/PCMA"}
	case astiav.CodecIDPcmS16Le, astiav.CodecIDPcmS16Be:
		return []string{"audio/L16"}
	}
	return nil
}

func (c *Codec) GetAndroidMIMEType() string {
	switch c.CodecContext().CodecID() {
	case astiav.CodecIDH264:
		return "video/avc"
	case astiav.CodecIDHevc:
		return "video/hevc"
	case astiav.CodecIDAv1:
		return "video/av01"
	case astiav.CodecIDMpeg4:
		return "video/mp4v-es"
	case astiav.CodecIDMpeg2Video:
		return "video/mpeg2"
	case astiav.CodecIDVp8:
		return "video/x-vnd.on2.vp8"
	case astiav.CodecIDVp9:
		return "video/x-vnd.on2.vp9"
	case astiav.CodecIDH263:
		return "video/3gpp"
	case astiav.CodecIDAac, astiav.CodecIDAacLatm:
		return "audio/mp4a-latm"
	case astiav.CodecIDMp3:
		return "audio/mpeg"
	case astiav.CodecIDOpus:
		return "audio/opus"
	case astiav.CodecIDVorbis:
		return "audio/vorbis"
	case astiav.CodecIDFlac:
		return "audio/flac"
	case astiav.CodecIDAc3:
		return "audio/ac3"
	case astiav.CodecIDAmrNb:
		return "audio/3gpp"
	case astiav.CodecIDAmrWb:
		return "audio/amr-wb"
	case astiav.CodecIDPcmMulaw:
		return "audio/g711-mlaw"
	case astiav.CodecIDPcmAlaw:
		return "audio/g711-alaw"
	case astiav.CodecIDPcmS16Le, astiav.CodecIDPcmS16Be,
		astiav.CodecIDPcmS32Le, astiav.CodecIDPcmS32Be,
		astiav.CodecIDPcmF32Le, astiav.CodecIDPcmF32Be:
		return "audio/raw"
	}
	return ""
}
