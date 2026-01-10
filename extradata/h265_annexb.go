// h265_annexb.go provides functionality for parsing and representing H.265 Annex-B sequences.

package extradata

import (
	"fmt"
	"strings"
)

type H265NalUnitType uint8

const (
	H265NalUnitTypeTrailN    H265NalUnitType = 0
	H265NalUnitTypeTrailR    H265NalUnitType = 1
	H265NalUnitTypeTSAN      H265NalUnitType = 2
	H265NalUnitTypeTSAR      H265NalUnitType = 3
	H265NalUnitTypeSTSAN     H265NalUnitType = 4
	H265NalUnitTypeSTSAR     H265NalUnitType = 5
	H265NalUnitTypeRADLN     H265NalUnitType = 6
	H265NalUnitTypeRADLR     H265NalUnitType = 7
	H265NalUnitTypeRASLN     H265NalUnitType = 8
	H265NalUnitTypeRASLR     H265NalUnitType = 9
	H265NalUnitTypeBLAWLP    H265NalUnitType = 16
	H265NalUnitTypeBLAWRADL  H265NalUnitType = 17
	H265NalUnitTypeBLANLP    H265NalUnitType = 18
	H265NalUnitTypeIDRWISCL  H265NalUnitType = 19
	H265NalUnitTypeIDRNLP    H265NalUnitType = 20
	H265NalUnitTypeCRAWNUT   H265NalUnitType = 21
	H265NalUnitTypeVPS       H265NalUnitType = 32
	H265NalUnitTypeSPS       H265NalUnitType = 33
	H265NalUnitTypePPS       H265NalUnitType = 34
	H265NalUnitTypeAUD       H265NalUnitType = 35
	H265NalUnitTypeEOS       H265NalUnitType = 36
	H265NalUnitTypeEOB       H265NalUnitType = 37
	H265NalUnitTypeFD        H265NalUnitType = 38
	H265NalUnitTypePrefixSEI H265NalUnitType = 39
	H265NalUnitTypeSuffixSEI H265NalUnitType = 40
)

type H265NALU struct {
	Raw  []byte
	Type H265NalUnitType
}

type H265AnnexB struct {
	Raw   []byte
	NALUs []H265NALU
}

func ParseH265AnnexB(b []byte) (*H265AnnexB, error) {
	naluBytes := SplitAnnexB(b)
	if len(naluBytes) == 0 {
		return nil, fmt.Errorf("no NAL units found")
	}

	seq := &H265AnnexB{
		Raw: append([]byte(nil), b...),
	}

	for _, nb := range naluBytes {
		if len(nb) < 2 {
			continue
		}
		seq.NALUs = append(seq.NALUs, H265NALU{
			Raw:  append([]byte(nil), nb...),
			Type: H265NalUnitType((nb[0] & 0x7E) >> 1),
		})
	}
	if len(seq.NALUs) == 0 {
		return nil, fmt.Errorf("no valid NAL units parsed")
	}

	return seq, nil
}

func (s *H265AnnexB) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "H.265 Annex-B sequence (%d NAL units)\n", len(s.NALUs))
	for i, n := range s.NALUs {
		preview := len(n.Raw)
		if preview > 16 {
			preview = 16
		}
		fmt.Fprintf(
			&sb,
			"  NALU[%d]: len=%d, type=%d (%s), first %d bytes=% X\n",
			i, len(n.Raw), int(n.Type), h265NalTypeName(n.Type),
			preview, n.Raw[:preview],
		)
	}
	return sb.String()
}

func h265NalTypeName(t H265NalUnitType) string {
	switch t {
	case H265NalUnitTypeTrailN, H265NalUnitTypeTrailR:
		return "TRAIL_N/TRAIL_R"
	case H265NalUnitTypeTSAN, H265NalUnitTypeTSAR:
		return "TSA_N/TSA_R"
	case H265NalUnitTypeSTSAN, H265NalUnitTypeSTSAR:
		return "STSA_N/STSA_R"
	case H265NalUnitTypeRADLN, H265NalUnitTypeRADLR:
		return "RADL_N/RADL_R"
	case H265NalUnitTypeRASLN, H265NalUnitTypeRASLR:
		return "RASL_N/RASL_R"
	case H265NalUnitTypeBLAWLP, H265NalUnitTypeBLAWRADL, H265NalUnitTypeBLANLP:
		return "BLA"
	case H265NalUnitTypeIDRWISCL, H265NalUnitTypeIDRNLP:
		return "IDR"
	case H265NalUnitTypeCRAWNUT:
		return "CRA"
	case H265NalUnitTypeVPS:
		return "VPS"
	case H265NalUnitTypeSPS:
		return "SPS"
	case H265NalUnitTypePPS:
		return "PPS"
	case H265NalUnitTypeAUD:
		return "AUD"
	case H265NalUnitTypeEOS:
		return "EOS"
	case H265NalUnitTypeEOB:
		return "EOB"
	case H265NalUnitTypeFD:
		return "FD" // Filler Data
	case H265NalUnitTypePrefixSEI, H265NalUnitTypeSuffixSEI:
		return "SEI"
	default:
		return "other"
	}
}
