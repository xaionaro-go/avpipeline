// extra_data.go provides types and functions for parsing and handling codec extradata.

// Package extradata provides types and functions for parsing and handling codec extradata.
package extradata

import (
	"bytes"
	"fmt"
)

type Raw []byte

func (b Raw) Equal(cmp Raw) bool {
	return bytes.Equal(b, cmp)
}

func (b Raw) String() string {
	if len(b) == 0 {
		return "<empty>"
	}

	return b.Parse().String()
}

// Parsed is the top-level "discriminated union".
type Parsed interface {
	fmt.Stringer
}

// Parse tries known formats (H.264 AVCC, AAC ASC, H.264 Annex-B)
// and falls back to Unknown.
func (b Raw) Parse() Parsed {
	if len(b) == 0 {
		return nil
	}

	if avcc, err := ParseH264AVCC(b); err == nil {
		return avcc
	}

	if aacasc, err := ParseAACASC(b); err == nil {
		return aacasc
	}

	if seq, err := ParseH264AnnexB(b); err == nil {
		return seq
	}

	if av1c, err := ParseAV1C(b); err == nil {
		return av1c
	}

	return Unknown(b)
}

type Unknown []byte

func (b Unknown) String() string {
	return fmt.Sprintf("<unknown_type, raw:%X>", len(b))
}
