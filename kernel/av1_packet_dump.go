package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/logger"
)

const (
	av1PacketDumpDirEnv = "AVPIPELINE_AV1_PACKET_DUMP_DIR"
	av1PacketDumpMaxEnv = "AVPIPELINE_AV1_PACKET_DUMP_MAX"

	av1PacketDumpDefaultMax             = 32
	av1PacketDumpExtraDataHexPrefixSize = 64
	av1PacketDumpFailedAttemptMax       = 3
)

type av1PacketDumpStage string

const (
	av1PacketDumpStagePostDemux  av1PacketDumpStage = "post-demux"
	av1PacketDumpStagePreDecoder av1PacketDumpStage = "pre-decoder"
)

type av1PacketDumpInput struct {
	Stage           av1PacketDumpStage
	Packet          *astiav.Packet
	CodecParameters *astiav.CodecParameters
	MediaType       astiav.MediaType
	StreamIndex     int
	TimeBase        astiav.Rational
}

type av1PacketDumpFailureKey struct {
	Stage av1PacketDumpStage
	Dir   string
}

type av1PacketDumpFailureAttempt struct {
	key      av1PacketDumpFailureKey
	finished bool
}

type av1PacketDumpRecord struct {
	Stage       string `json:"stage"`
	Sequence    uint64 `json:"sequence"`
	CodecID     int    `json:"codec_id"`
	CodecName   string `json:"codec_name"`
	MediaType   string `json:"media_type"`
	StreamIndex int    `json:"stream_index"`

	PacketSize   int    `json:"packet_size"`
	PacketSHA256 string `json:"packet_sha256"`
	PacketFile   string `json:"packet_file"`

	PTS         int64  `json:"pts"`
	DTS         int64  `json:"dts"`
	Duration    int64  `json:"duration"`
	Pos         int64  `json:"pos"`
	TimeBase    string `json:"time_base"`
	TimeBaseNum int    `json:"time_base_num"`
	TimeBaseDen int    `json:"time_base_den"`
	Flags       int    `json:"flags"`
	KeyFrame    bool   `json:"keyframe"`

	ExtraDataSize           int                  `json:"extradata_size"`
	ExtraDataSHA256         string               `json:"extradata_sha256"`
	ExtraDataHexPrefix      string               `json:"extradata_hex_prefix"`
	ExtraDataAV1CParses     bool                 `json:"extradata_av1c_parses"`
	ExtraDataAV1CParseError string               `json:"extradata_av1c_parse_error,omitempty"`
	ExtraDataAV1C           *av1PacketDumpAV1C   `json:"extradata_av1c,omitempty"`
	PacketNewExtraData      *av1PacketDumpBuffer `json:"packet_new_extradata,omitempty"`
}

type av1PacketDumpAV1C struct {
	Marker                          uint8 `json:"marker"`
	Version                         uint8 `json:"version"`
	SeqProfile                      uint8 `json:"seq_profile"`
	SeqLevelIdx0                    uint8 `json:"seq_level_idx_0"`
	SeqTier0                        uint8 `json:"seq_tier_0"`
	HighBitDepth                    bool  `json:"high_bit_depth"`
	TwelveBit                       bool  `json:"twelve_bit"`
	Monochrome                      bool  `json:"monochrome"`
	ChromaSubsamplingX              uint8 `json:"chroma_subsampling_x"`
	ChromaSubsamplingY              uint8 `json:"chroma_subsampling_y"`
	ChromaSamplePosition            uint8 `json:"chroma_sample_position"`
	InitialPresentationDelayPresent bool  `json:"initial_presentation_delay_present"`
	InitialPresentationDelayMinus1  uint8 `json:"initial_presentation_delay_minus_1"`
	BitDepth                        int   `json:"bit_depth"`
	ConfigOBUsSize                  int   `json:"config_obus_size"`
}

type av1PacketDumpBuffer struct {
	Size           int    `json:"size"`
	SHA256         string `json:"sha256"`
	HexPrefix      string `json:"hex_prefix"`
	AV1CParses     bool   `json:"av1c_parses"`
	AV1CParseError string `json:"av1c_parse_error,omitempty"`
}

var (
	av1PacketDumpMu                    sync.Mutex
	av1PacketDumpCounts                = map[av1PacketDumpStage]uint64{}
	av1PacketDumpInFlightCounts        = map[av1PacketDumpStage]uint64{}
	av1PacketDumpSequenceCounts        = map[av1PacketDumpStage]uint64{}
	av1PacketDumpFailureCounts         = map[av1PacketDumpFailureKey]uint64{}
	av1PacketDumpFailureInFlightCounts = map[av1PacketDumpFailureKey]uint64{}
	av1PacketDumpMaxParsed             bool
	av1PacketDumpMaxValue              uint64
)

func dumpAV1Packet(
	ctx context.Context,
	input av1PacketDumpInput,
) bool {
	dir := os.Getenv(av1PacketDumpDirEnv)
	if dir == "" {
		return false
	}

	if !input.isAV1VideoPacket() {
		return false
	}

	max := av1PacketDumpMax(ctx)
	if !hasAV1PacketDumpBudget(input.Stage, max) {
		return false
	}

	failureAttempt, ok := reserveAV1PacketDumpFailureAttempt(input.Stage, dir)
	if !ok {
		return false
	}
	defer failureAttempt.finish(false)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		failureCount := failureAttempt.finish(true)
		logger.Warnf(
			ctx,
			"unable to create AV1 packet dump directory %q (stage=%s failed_attempt=%d/%d): %v",
			dir,
			input.Stage,
			failureCount,
			av1PacketDumpFailedAttemptMax,
			err,
		)
		return false
	}

	seq, ok := reserveAV1PacketDumpSequence(input.Stage, max)
	if !ok {
		return false
	}
	dumped := false
	defer func() {
		finishAV1PacketDumpSequence(input.Stage, dumped)
	}()

	packetFile := string(input.Stage) + "-" + zeroPadAV1PacketDumpSequence(seq) + ".bin"
	recordFile := string(input.Stage) + "-" + zeroPadAV1PacketDumpSequence(seq) + ".json"
	record := input.record(seq, packetFile)
	packetPath := filepath.Join(dir, packetFile)
	if err := writeAV1PacketDumpFile(packetPath, input.Packet.Data()); err != nil {
		failureCount := failureAttempt.finish(true)
		logger.Warnf(
			ctx,
			"unable to write AV1 packet dump %q (stage=%s dir=%q failed_attempt=%d/%d): %v",
			packetFile,
			input.Stage,
			dir,
			failureCount,
			av1PacketDumpFailedAttemptMax,
			err,
		)
		return false
	}

	recordBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		failureCount := failureAttempt.finish(true)
		logger.Warnf(
			ctx,
			"unable to marshal AV1 packet dump metadata for %q (stage=%s dir=%q failed_attempt=%d/%d): %v",
			recordFile,
			input.Stage,
			dir,
			failureCount,
			av1PacketDumpFailedAttemptMax,
			err,
		)
		return false
	}
	recordBytes = append(recordBytes, '\n')
	if err := writeAV1PacketDumpFile(filepath.Join(dir, recordFile), recordBytes); err != nil {
		_ = os.Remove(packetPath)
		failureCount := failureAttempt.finish(true)
		logger.Warnf(
			ctx,
			"unable to write AV1 packet dump metadata %q (stage=%s dir=%q failed_attempt=%d/%d): %v",
			recordFile,
			input.Stage,
			dir,
			failureCount,
			av1PacketDumpFailedAttemptMax,
			err,
		)
		return false
	}

	dumped = true
	failureAttempt.finish(false)
	resetAV1PacketDumpFailures(input.Stage, dir)
	logger.Tracef(ctx, "wrote AV1 packet dump stage=%s sequence=%d dir=%q", input.Stage, seq, dir)
	return true
}

func (input av1PacketDumpInput) isAV1VideoPacket() bool {
	if input.Packet == nil {
		return false
	}
	if input.CodecParameters == nil {
		return false
	}
	return input.MediaType == astiav.MediaTypeVideo && input.CodecParameters.CodecID() == astiav.CodecIDAv1
}

func hasAV1PacketDumpBudget(
	stage av1PacketDumpStage,
	max uint64,
) bool {
	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	return av1PacketDumpCounts[stage]+av1PacketDumpInFlightCounts[stage] < max
}

func reserveAV1PacketDumpSequence(
	stage av1PacketDumpStage,
	max uint64,
) (uint64, bool) {
	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	if av1PacketDumpCounts[stage]+av1PacketDumpInFlightCounts[stage] >= max {
		return 0, false
	}
	av1PacketDumpInFlightCounts[stage]++
	av1PacketDumpSequenceCounts[stage]++
	return av1PacketDumpSequenceCounts[stage], true
}

func finishAV1PacketDumpSequence(
	stage av1PacketDumpStage,
	dumped bool,
) {
	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	if av1PacketDumpInFlightCounts[stage] > 0 {
		av1PacketDumpInFlightCounts[stage]--
	}
	if dumped {
		av1PacketDumpCounts[stage]++
	}
}

func reserveAV1PacketDumpFailureAttempt(
	stage av1PacketDumpStage,
	dir string,
) (*av1PacketDumpFailureAttempt, bool) {
	key := av1PacketDumpFailureKey{
		Stage: stage,
		Dir:   dir,
	}

	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	if av1PacketDumpFailureCounts[key]+av1PacketDumpFailureInFlightCounts[key] >= av1PacketDumpFailedAttemptMax {
		return nil, false
	}
	av1PacketDumpFailureInFlightCounts[key]++
	return &av1PacketDumpFailureAttempt{
		key: key,
	}, true
}

func (attempt *av1PacketDumpFailureAttempt) finish(failed bool) uint64 {
	if attempt == nil {
		return 0
	}
	if attempt.finished {
		return 0
	}

	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	attempt.finished = true
	switch count := av1PacketDumpFailureInFlightCounts[attempt.key]; {
	case count > 1:
		av1PacketDumpFailureInFlightCounts[attempt.key] = count - 1
	case count == 1:
		delete(av1PacketDumpFailureInFlightCounts, attempt.key)
	}
	if failed {
		av1PacketDumpFailureCounts[attempt.key]++
	}
	return av1PacketDumpFailureCounts[attempt.key]
}

func resetAV1PacketDumpFailures(
	stage av1PacketDumpStage,
	dir string,
) {
	key := av1PacketDumpFailureKey{
		Stage: stage,
		Dir:   dir,
	}

	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	delete(av1PacketDumpFailureCounts, key)
}

func av1PacketDumpMax(ctx context.Context) uint64 {
	av1PacketDumpMu.Lock()
	if av1PacketDumpMaxParsed {
		max := av1PacketDumpMaxValue
		av1PacketDumpMu.Unlock()
		return max
	}

	raw := os.Getenv(av1PacketDumpMaxEnv)
	max := uint64(av1PacketDumpDefaultMax)
	var parseErr error
	if raw != "" {
		max, parseErr = strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			max = av1PacketDumpDefaultMax
		}
	}
	av1PacketDumpMaxValue = max
	av1PacketDumpMaxParsed = true
	av1PacketDumpMu.Unlock()

	if parseErr != nil {
		logger.Warnf(ctx, "unable to parse %s=%q, using %d: %v", av1PacketDumpMaxEnv, raw, av1PacketDumpDefaultMax, parseErr)
	}
	return max
}

func writeAV1PacketDumpFile(
	path string,
	data []byte,
) (_err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil && _err == nil {
			_err = err
		}
		if _err != nil {
			_ = os.Remove(path)
		}
	}()

	_, err = file.Write(data)
	return err
}

func (input av1PacketDumpInput) record(
	seq uint64,
	packetFile string,
) av1PacketDumpRecord {
	packetData := input.Packet.Data()
	extraData := input.CodecParameters.ExtraData()
	codecID := input.CodecParameters.CodecID()
	flags := input.Packet.Flags()
	record := av1PacketDumpRecord{
		Stage:       string(input.Stage),
		Sequence:    seq,
		CodecID:     int(codecID),
		CodecName:   codecID.String(),
		MediaType:   input.MediaType.String(),
		StreamIndex: input.StreamIndex,

		PacketSize:   input.Packet.Size(),
		PacketSHA256: av1PacketDumpSHA256Hex(packetData),
		PacketFile:   packetFile,

		PTS:         input.Packet.Pts(),
		DTS:         input.Packet.Dts(),
		Duration:    input.Packet.Duration(),
		Pos:         input.Packet.Pos(),
		TimeBase:    input.TimeBase.String(),
		TimeBaseNum: input.TimeBase.Num(),
		TimeBaseDen: input.TimeBase.Den(),
		Flags:       int(flags),
		KeyFrame:    flags.Has(astiav.PacketFlagKey),

		ExtraDataSize:      len(extraData),
		ExtraDataSHA256:    av1PacketDumpSHA256Hex(extraData),
		ExtraDataHexPrefix: av1PacketDumpHexPrefix(extraData),
	}
	record.ExtraDataAV1C, record.ExtraDataAV1CParses, record.ExtraDataAV1CParseError = parseAV1PacketDumpAV1C(extraData)
	if newExtraData, ok := input.Packet.SideData().NewExtraData().Get(); ok {
		record.PacketNewExtraData = &av1PacketDumpBuffer{
			Size:      len(newExtraData),
			SHA256:    av1PacketDumpSHA256Hex(newExtraData),
			HexPrefix: av1PacketDumpHexPrefix(newExtraData),
		}
		_, record.PacketNewExtraData.AV1CParses, record.PacketNewExtraData.AV1CParseError = parseAV1PacketDumpAV1C(newExtraData)
	}
	return record
}

func parseAV1PacketDumpAV1C(data []byte) (*av1PacketDumpAV1C, bool, string) {
	av1c, err := extradata.ParseAV1C(data)
	if err != nil {
		return nil, false, err.Error()
	}
	return &av1PacketDumpAV1C{
		Marker:                          av1c.Marker,
		Version:                         av1c.Version,
		SeqProfile:                      av1c.SeqProfile,
		SeqLevelIdx0:                    av1c.SeqLevelIdx0,
		SeqTier0:                        av1c.SeqTier0,
		HighBitDepth:                    av1c.HighBitDepth,
		TwelveBit:                       av1c.TwelveBit,
		Monochrome:                      av1c.Monochrome,
		ChromaSubsamplingX:              av1c.ChromaSubsamplingX,
		ChromaSubsamplingY:              av1c.ChromaSubsamplingY,
		ChromaSamplePosition:            av1c.ChromaSamplePosition,
		InitialPresentationDelayPresent: av1c.InitialPresentationDelayPresent,
		InitialPresentationDelayMinus1:  av1c.InitialPresentationDelayMinus1,
		BitDepth:                        av1c.BitDepth(),
		ConfigOBUsSize:                  len(av1c.ConfigOBUs),
	}, true, ""
}

func av1PacketDumpSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func av1PacketDumpHexPrefix(data []byte) string {
	if len(data) > av1PacketDumpExtraDataHexPrefixSize {
		data = data[:av1PacketDumpExtraDataHexPrefixSize]
	}
	return hex.EncodeToString(data)
}

func zeroPadAV1PacketDumpSequence(seq uint64) string {
	return fmt.Sprintf("%06d", seq)
}
