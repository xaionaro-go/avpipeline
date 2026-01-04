// sender_props.go defines the properties of a stream sender.

package types

type SenderNodeProps struct{}

type SenderProps struct {
	TranscoderConfig
	SenderNodeProps
}
