// Package packet provides functions for processing media packet data.
package packet

import (
	"fmt"
	"iter"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/extradata"
)

// NALFormat represents how NAL units are framed in packet data.
// Zero means Annex-B (start-code delimited). A positive value means
// length-prefixed with that many bytes per length field.
type NALFormat int

const (
	// NALFormatAnnexB indicates Annex-B framing (00 00 00 01 / 00 00 01 start codes).
	NALFormatAnnexB NALFormat = 0

	// NALFormatLengthPrefixed4 indicates 4-byte big-endian length-prefixed
	// framing (AVCC for H.264, HVCC for H.265).
	NALFormatLengthPrefixed4 NALFormat = 4
)

// DetectNALFormat returns the framing format of the given NAL unit data.
func DetectNALFormat(data []byte) NALFormat {
	if extradata.IsAnnexB(data) {
		return NALFormatAnnexB
	}
	return NALFormatLengthPrefixed4
}

// NALU represents a format-agnostic NAL unit.
type NALU struct {
	Raw  []byte
	Type uint64
}

// Iter returns an iterator over NAL units in the given packet data.
// It auto-detects whether the data uses Annex-B or length-prefixed framing.
func Iter(codecID astiav.CodecID, data []byte) iter.Seq2[NALU, error] {
	return func(yield func(NALU, error) bool) {
		switch codecID {
		case astiav.CodecIDH264:
			iterH264(data, yield)
		case astiav.CodecIDHevc:
			iterH265(data, yield)
		default:
			yield(NALU{}, fmt.Errorf("unsupported codec ID: %v", codecID))
		}
	}
}

func iterH264(data []byte, yield func(NALU, error) bool) {
	if extradata.IsAnnexB(data) {
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
		return
	}

	// Length-prefixed (AVCC) format.
	nalus, err := extradata.SplitLengthPrefixed(data, int(NALFormatLengthPrefixed4))
	if err != nil {
		yield(NALU{}, fmt.Errorf("parse H.264 length-prefixed: %w", err))
		return
	}
	for _, nb := range nalus {
		if len(nb) == 0 {
			continue
		}
		if !yield(NALU{Raw: nb, Type: uint64(extradata.H264NalUnitType(nb[0] & 0x1F))}, nil) {
			return
		}
	}
}

func iterH265(data []byte, yield func(NALU, error) bool) {
	if extradata.IsAnnexB(data) {
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
		return
	}

	// Length-prefixed (HVCC) format.
	nalus, err := extradata.SplitLengthPrefixed(data, int(NALFormatLengthPrefixed4))
	if err != nil {
		yield(NALU{}, fmt.Errorf("parse H.265 length-prefixed: %w", err))
		return
	}
	for _, nb := range nalus {
		if len(nb) < 2 {
			continue
		}
		if !yield(NALU{Raw: nb, Type: uint64(extradata.H265NalUnitType((nb[0] & 0x7E) >> 1))}, nil) {
			return
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

// JoinNALUsFormat joins NAL units in the specified format.
// Use this to preserve the original framing when round-tripping
// through Iter → filter → rejoin.
func JoinNALUsFormat(nalus []NALU, format NALFormat) []byte {
	switch format {
	case NALFormatAnnexB:
		return JoinNALUs(nalus)
	default:
		rawSlices := make([][]byte, len(nalus))
		for i, n := range nalus {
			rawSlices[i] = n.Raw
		}
		return extradata.JoinLengthPrefixed(rawSlices, int(format))
	}
}
