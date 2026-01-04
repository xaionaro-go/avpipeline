// type.go defines the resource Type enum.

package resource

import (
	"fmt"
)

type Type int

const (
	UndefinedType Type = iota
	TypeDecoder
	TypeEncoder
	EndOfType
)

func (t Type) String() string {
	switch t {
	case UndefinedType:
		return "<undefined>"
	case TypeDecoder:
		return "decoder"
	case TypeEncoder:
		return "encoder"
	default:
		return fmt.Sprintf("<unexpected_%d>", int(t))
	}
}
