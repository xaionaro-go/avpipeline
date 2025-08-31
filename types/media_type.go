package types

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
