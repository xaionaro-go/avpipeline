package bitstreamfilter

import (
	"github.com/asticode/go-astiav"
)

type Name string

const (
	NameAACADTSToASC       = Name("aac_adtstoasc")
	NameAV1FrameMerge      = Name("av1_frame_merge")
	NameAV1FrameSplit      = Name("av1_frame_split")
	NameAV1Metadata        = Name("av1_metadata")
	NameChomp              = Name("chomp")
	NameDCACore            = Name("dca_core")
	NameDoviRpu            = Name("dovi_rpu")
	NameDTS2PTS            = Name("dts2pts")
	NameDumpExtra          = Name("dump_extra")
	NameDVErrorMarker      = Name("dv_error_marker")
	NameEAC3Core           = Name("eac3_core")
	NameEVCFrameMerge      = Name("evc_frame_merge")
	NameExtractExtradata   = Name("extract_extradata")
	NameFilterUnits        = Name("filter_units")
	NameH264Metadata       = Name("h264_metadata")
	NameH264MP4toAnnexB    = Name("h264_mp4toannexb")
	NameH264RedundantPps   = Name("h264_redundant_pps")
	NameHAPQAExtract       = Name("hapqa_extract")
	NameHEVCMetadata       = Name("hevc_metadata")
	NameHEVCMP4toAnnexB    = Name("hevc_mp4toannexb")
	NameIMXDump            = Name("imxdump")
	NameMedia100ToMJPEGb   = Name("media100_to_mjpegb")
	NameMJPEG2JPEG         = Name("mjpeg2jpeg")
	NameMJPEGADump         = Name("mjpegadump")
	NameMOV2TextSub        = Name("mov2textsub")
	NameMPEG2Metadata      = Name("mpeg2_metadata")
	NameMPEG4UnpackBFrames = Name("mpeg4_unpack_bframes")
	NameNoise              = Name("noise")
	NameNull               = Name("null")
	NameOpusMetadata       = Name("opus_metadata")
	NamePCMRechunk         = Name("pcm_rechunk")
	NamePGSFrameMerge      = Name("pgs_frame_merge")
	NameProresMetadata     = Name("prores_metadata")
	NameRemoveExtra        = Name("remove_extra")
	NameSetTS              = Name("setts")
	NameShowinfo           = Name("showinfo")
	NameText2movsub        = Name("text2movsub")
	NameTraceHeaders       = Name("trace_headers")
	NameTrueHDCore         = Name("truehd_core")
	NameVP9Metadata        = Name("vp9_metadata")
	NameVP9RawReorder      = Name("vp9_raw_reorder")
	NameVP9Superframe      = Name("vp9_superframe")
	NameVP9SuperframeSplit = Name("vp9_superframe_split")
	NameVVCMetadata        = Name("vvc_metadata")
	NameVVCMP4toAnnexB     = Name("vvc_mp4toannexb")
)

func NameMP4ToAnnexB(codecID astiav.CodecID) Name {
	switch codecID {
	case astiav.CodecIDH264:
		return NameH264MP4toAnnexB
	case astiav.CodecIDHevc:
		return NameHEVCMP4toAnnexB
		// TODO: add the case for 'NameVVCMP4toAnnexB'
	}
	return NameNull
}
