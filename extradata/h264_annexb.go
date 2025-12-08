package extradata

import (
	"fmt"
	"strings"
)

type H264NALU struct {
	Raw  []byte
	Type int
	NRI  int
}

type H264AnnexB struct {
	Raw   []byte
	NALUs []H264NALU
}

func ParseH264AnnexB(b []byte) (*H264AnnexB, error) {
	naluBytes := splitAnnexB(b)
	if len(naluBytes) == 0 {
		return nil, fmt.Errorf("no NAL units found")
	}

	seq := &H264AnnexB{
		Raw: append([]byte(nil), b...),
	}

	for _, nb := range naluBytes {
		if len(nb) == 0 {
			continue
		}
		h := nb[0]
		seq.NALUs = append(seq.NALUs, H264NALU{
			Raw:  append([]byte(nil), nb...),
			Type: int(h & 0x1F),
			NRI:  int((h >> 5) & 0x03),
		})
	}
	if len(seq.NALUs) == 0 {
		return nil, fmt.Errorf("no valid NAL units parsed")
	}

	return seq, nil
}

func (s *H264AnnexB) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "H.264 Annex-B sequence (%d NAL units)\n", len(s.NALUs))
	for i, n := range s.NALUs {
		preview := len(n.Raw)
		if preview > 16 {
			preview = 16
		}
		fmt.Fprintf(
			&sb,
			"  NALU[%d]: len=%d, type=%d (%s), NRI=%d, first %d bytes=% X\n",
			i, len(n.Raw), n.Type, h264NalTypeName(n.Type), n.NRI,
			preview, n.Raw[:preview],
		)
	}
	return sb.String()
}

func splitAnnexB(b []byte) [][]byte {
	var nalus [][]byte
	n := len(b)
	i := 0

	for {
		start := findStartCode(b, i)
		if start < 0 {
			break
		}

		// length of start code (3 or 4 bytes)
		scLen := 3
		if start+3 < n &&
			b[start] == 0 && b[start+1] == 0 &&
			b[start+2] == 0 && b[start+3] == 1 {
			scLen = 4
		}

		next := findStartCode(b, start+scLen)
		end := n
		if next >= 0 {
			end = next
		}

		if start+scLen >= end {
			if next < 0 {
				break
			}
			i = next
			continue
		}

		nalu := b[start+scLen : end]
		if len(nalu) > 0 {
			cloned := make([]byte, len(nalu))
			copy(cloned, nalu)
			nalus = append(nalus, cloned)
		}

		if next < 0 {
			break
		}
		i = next
	}

	return nalus
}

func findStartCode(b []byte, start int) int {
	n := len(b)
	for i := start; i+3 <= n; i++ {
		if b[i] == 0 && b[i+1] == 0 {
			// 00 00 01
			if b[i+2] == 1 {
				return i
			}
			// 00 00 00 01
			if i+4 <= n && b[i+2] == 0 && b[i+3] == 1 {
				return i
			}
		}
	}
	return -1
}

func h264NalTypeName(t int) string {
	switch t {
	case 1:
		return "non-IDR slice"
	case 2:
		return "slice data A"
	case 3:
		return "slice data B"
	case 4:
		return "slice data C"
	case 5:
		return "IDR slice"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	case 10:
		return "end of sequence"
	case 11:
		return "end of stream"
	case 12:
		return "filler"
	default:
		return "reserved/unknown"
	}
}
