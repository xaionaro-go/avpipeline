// Package packet provides functions for processing media packet data.
package packet

import (
	"fmt"
	"iter"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/extradata"
)

// NALU represents a format-agnostic NAL unit.
type NALU struct {
	Raw  []byte
	Type uint64
}

// Iter returns an iterator over NAL units in the given Annex-B data.
func Iter(codecID astiav.CodecID, data []byte) iter.Seq2[NALU, error] {
	return func(yield func(NALU, error) bool) {
		switch codecID {
		case astiav.CodecIDH264:
			seq, err := extradata.ParseH264AnnexB(data)
			if err != nil {
				yield(NALU{}, fmt.Errorf("parse H.264 Annex-B: %w", err))
				return
			}
			for _, n := range seq.NALUs {
				if !yield(NALU{Raw: n.Raw, Type: uint64(n.Type)}, nil) {
					return
				}
			}
		case astiav.CodecIDHevc:
			seq, err := extradata.ParseH265AnnexB(data)
			if err != nil {
				yield(NALU{}, fmt.Errorf("parse H.265 Annex-B: %w", err))
				return
			}
			for _, n := range seq.NALUs {
				if !yield(NALU{Raw: n.Raw, Type: uint64(n.Type)}, nil) {
					return
				}
			}
		default:
			yield(NALU{}, fmt.Errorf("unsupported codec ID: %v", codecID))
		}
	}
}

// IsFiller returns true if the NALU is a filler NAL unit for the given codec.
func IsFiller(codecID astiav.CodecID, naluType uint64) bool {
	switch codecID {
	case astiav.CodecIDH264:
		return naluType == uint64(extradata.H264NalUnitTypeFiller)
	case astiav.CodecIDHevc:
		return naluType == uint64(extradata.H265NalUnitTypeFD)
	default:
		return false
	}
}

// JoinNALUs joins NAL units with Annex-B start codes.
func JoinNALUs(nalus []NALU) []byte {
	var size int
	for _, nalu := range nalus {
		size += 4 + len(nalu.Raw)
	}
	result := make([]byte, 0, size)
	for _, nalu := range nalus {
		result = append(result, 0, 0, 0, 1)
		result = append(result, nalu.Raw...)
	}
	return result
}
