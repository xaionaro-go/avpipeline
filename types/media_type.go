// media_type.go defines the MediaType enum and its methods.

package types

import "fmt"

type MediaType int

const (
	MediaTypeAttachment = MediaType(0x4)
	MediaTypeAudio      = MediaType(0x1)
	MediaTypeData       = MediaType(0x2)
	MediaTypeNb         = MediaType(0x5)
	MediaTypeSubtitle   = MediaType(0x3)
	MediaTypeUnknown    = MediaType(-0x1)
	MediaTypeVideo      = MediaType(0x0)
)

func MediaTypes() []MediaType {
	return []MediaType{
		MediaTypeUnknown,
		MediaTypeVideo,
		MediaTypeAudio,
		MediaTypeSubtitle,
	}
}

func (t MediaType) String() string {
	switch t {
	case MediaTypeAttachment:
		return "attachment"
	case MediaTypeAudio:
		return "audio"
	case MediaTypeData:
		return "data"
	case MediaTypeSubtitle:
		return "subtitle"
	case MediaTypeVideo:
		return "video"
	case MediaTypeUnknown:
		return "unknown"
	default:
		return "MediaType(" + fmt.Sprintf("%d", int(t)) + ")"
	}
}
