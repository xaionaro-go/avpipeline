// length_prefixed.go provides functions for detecting and splitting
// length-prefixed NAL unit data (AVCC/HVCC packet format), as opposed
// to Annex-B (start-code-delimited) format.

package extradata

import (
	"encoding/binary"
	"fmt"
)

// IsAnnexB reports whether data begins with an Annex-B start code
// (00 00 01 or 00 00 00 01).
func IsAnnexB(b []byte) bool {
	if len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1 {
		return true
	}
	if len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1 {
		return true
	}
	return false
}

// SplitLengthPrefixed splits NAL units from length-prefixed data where
// each NAL unit is preceded by a big-endian length field of nalLengthSize
// bytes (1, 2, 3, or 4).
func SplitLengthPrefixed(
	b []byte,
	nalLengthSize int,
) ([][]byte, error) {
	switch nalLengthSize {
	case 1, 2, 3, 4:
	default:
		return nil, fmt.Errorf("unsupported NAL length size: %d", nalLengthSize)
	}

	var nalus [][]byte
	offset := 0
	for offset < len(b) {
		if offset+nalLengthSize > len(b) {
			return nalus, fmt.Errorf(
				"truncated length prefix at offset %d (need %d bytes, have %d)",
				offset, nalLengthSize, len(b)-offset,
			)
		}
		var naluLen uint32
		switch nalLengthSize {
		case 1:
			naluLen = uint32(b[offset])
		case 2:
			naluLen = uint32(binary.BigEndian.Uint16(b[offset:]))
		case 3:
			naluLen = uint32(b[offset])<<16 | uint32(b[offset+1])<<8 | uint32(b[offset+2])
		case 4:
			naluLen = binary.BigEndian.Uint32(b[offset:])
		}
		offset += nalLengthSize
		if naluLen == 0 {
			return nalus, fmt.Errorf("zero-length NAL unit at offset %d", offset-nalLengthSize)
		}
		if offset+int(naluLen) > len(b) {
			return nalus, fmt.Errorf(
				"NAL unit length %d at offset %d exceeds data bounds (%d bytes remain)",
				naluLen, offset-nalLengthSize, len(b)-offset,
			)
		}
		nalu := make([]byte, naluLen)
		copy(nalu, b[offset:offset+int(naluLen)])
		nalus = append(nalus, nalu)
		offset += int(naluLen)
	}
	return nalus, nil
}

// JoinLengthPrefixed joins raw NAL unit byte slices back into
// length-prefixed format using big-endian length fields of nalLengthSize bytes.
func JoinLengthPrefixed(
	nalus [][]byte,
	nalLengthSize int,
) []byte {
	switch nalLengthSize {
	case 1, 2, 3, 4:
	default:
		panic(fmt.Sprintf("unsupported NAL length size: %d", nalLengthSize))
	}

	var size int
	for _, nalu := range nalus {
		size += nalLengthSize + len(nalu)
	}
	result := make([]byte, 0, size)
	for _, nalu := range nalus {
		n := len(nalu)
		switch nalLengthSize {
		case 4:
			result = append(result, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		case 3:
			result = append(result, byte(n>>16), byte(n>>8), byte(n))
		case 2:
			result = append(result, byte(n>>8), byte(n))
		case 1:
			result = append(result, byte(n))
		}
		result = append(result, nalu...)
	}
	return result
}
