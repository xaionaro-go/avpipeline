// h264_annexb.go provides functionality for parsing and representing H.264 Annex-B sequences.

package extradata

import (
	"fmt"
	"strings"
)

type H264NalUnitType uint8

const (
	H264NalUnitTypeUnspecified       H264NalUnitType = 0
	H264NalUnitTypeNonIDR            H264NalUnitType = 1
	H264NalUnitTypeDataA             H264NalUnitType = 2
	H264NalUnitTypeDataB             H264NalUnitType = 3
	H264NalUnitTypeDataC             H264NalUnitType = 4
	H264NalUnitTypeIDR               H264NalUnitType = 5
	H264NalUnitTypeSEI               H264NalUnitType = 6
	H264NalUnitTypeSPS               H264NalUnitType = 7
	H264NalUnitTypePPS               H264NalUnitType = 8
	H264NalUnitTypeAUD               H264NalUnitType = 9
	H264NalUnitTypeEndOfSequence     H264NalUnitType = 10
	H264NalUnitTypeEndOfStream       H264NalUnitType = 11
	H264NalUnitTypeFiller            H264NalUnitType = 12
	H264NalUnitTypeSPSExt            H264NalUnitType = 13
	H264NalUnitTypePrefix            H264NalUnitType = 14
	H264NalUnitTypeSubsetSPS         H264NalUnitType = 15
	H264NalUnitTypeDepthParameterSet H264NalUnitType = 16
	H264NalUnitTypeReserved17        H264NalUnitType = 17
	H264NalUnitTypeReserved18        H264NalUnitType = 18
	H264NalUnitTypeAuxiliary         H264NalUnitType = 19
	H264NalUnitTypeExtension         H264NalUnitType = 20
	H264NalUnitTypeDepthNonIDR       H264NalUnitType = 21
)

type H264NALU struct {
	Raw  []byte
	Type H264NalUnitType
	NRI  int
}

type H264AnnexB struct {
	Raw   []byte
	NALUs []H264NALU
}

func ParseH264AnnexB(b []byte) (*H264AnnexB, error) {
	naluBytes := SplitAnnexB(b)
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
			Type: H264NalUnitType(h & 0x1F),
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
			i, len(n.Raw), int(n.Type), h264NalTypeName(n.Type), n.NRI,
			preview, n.Raw[:preview],
		)
	}
	return sb.String()
}

func h264NalTypeName(t H264NalUnitType) string {
	switch t {
	case H264NalUnitTypeNonIDR:
		return "non-IDR slice"
	case H264NalUnitTypeDataA:
		return "slice data A"
	case H264NalUnitTypeDataB:
		return "slice data B"
	case H264NalUnitTypeDataC:
		return "slice data C"
	case H264NalUnitTypeIDR:
		return "IDR slice"
	case H264NalUnitTypeSEI:
		return "SEI"
	case H264NalUnitTypeSPS:
		return "SPS"
	case H264NalUnitTypePPS:
		return "PPS"
	case H264NalUnitTypeAUD:
		return "AUD"
	case H264NalUnitTypeEndOfSequence:
		return "end of sequence"
	case H264NalUnitTypeEndOfStream:
		return "end of stream"
	case H264NalUnitTypeFiller:
		return "filler"
	default:
		return "reserved/unknown"
	}
}
