package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asticode/go-astiav"
	beltlogger "github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
)

func TestDumpAV1PacketDisabledByDefaultWritesNothing(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	dir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, "")

	pkt := newAV1DumpTestPacket(t, []byte{0x12, 0x34}, 100)
	defer pkt.Free()
	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	dumped := dumpAV1Packet(context.Background(), av1PacketDumpInput{
		Stage:           av1PacketDumpStagePostDemux,
		Packet:          pkt,
		CodecParameters: codecParams,
		MediaType:       astiav.MediaTypeVideo,
		StreamIndex:     3,
		TimeBase:        astiav.NewRational(1, 90000),
	})

	require.False(t, dumped)
	require.Empty(t, readAV1DumpTestDir(t, dir))
}

func TestDumpAV1PacketEnabledHonorsPerStageBoundAndWritesMetadata(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	dir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, dir)
	t.Setenv(av1PacketDumpMaxEnv, "2")

	extraData := []byte{0x81, 0x00, 0x00, 0x00}
	codecParams := newAV1DumpTestCodecParameters(t, extraData)
	defer codecParams.Free()

	payloads := [][]byte{
		{0x12, 0x34, 0x56},
		{0xab, 0xcd},
		{0xee},
	}
	for idx, payload := range payloads {
		pkt := newAV1DumpTestPacket(t, payload, int64(100+idx))
		dumped := dumpAV1Packet(context.Background(), av1PacketDumpInput{
			Stage:           av1PacketDumpStagePostDemux,
			Packet:          pkt,
			CodecParameters: codecParams,
			MediaType:       astiav.MediaTypeVideo,
			StreamIndex:     3,
			TimeBase:        astiav.NewRational(1, 90000),
		})
		pkt.Free()

		require.Equal(t, idx < 2, dumped)
	}

	preDecoderPacket := newAV1DumpTestPacket(t, []byte{0x99}, 200)
	defer preDecoderPacket.Free()
	require.True(t, dumpAV1Packet(context.Background(), av1PacketDumpInput{
		Stage:           av1PacketDumpStagePreDecoder,
		Packet:          preDecoderPacket,
		CodecParameters: codecParams,
		MediaType:       astiav.MediaTypeVideo,
		StreamIndex:     3,
		TimeBase:        astiav.NewRational(1, 90000),
	}))

	files := readAV1DumpTestDir(t, dir)
	require.ElementsMatch(t, []string{
		"post-demux-000001.bin",
		"post-demux-000001.json",
		"post-demux-000002.bin",
		"post-demux-000002.json",
		"pre-decoder-000001.bin",
		"pre-decoder-000001.json",
	}, files)

	record := readAV1DumpTestRecord(t, filepath.Join(dir, "post-demux-000001.json"))
	require.Equal(t, string(av1PacketDumpStagePostDemux), record.Stage)
	require.Equal(t, uint64(1), record.Sequence)
	require.Equal(t, int(astiav.CodecIDAv1), record.CodecID)
	require.Equal(t, astiav.CodecIDAv1.String(), record.CodecName)
	require.Equal(t, astiav.MediaTypeVideo.String(), record.MediaType)
	require.Equal(t, 3, record.StreamIndex)
	require.Equal(t, len(payloads[0]), record.PacketSize)
	require.Equal(t, sha256Hex(payloads[0]), record.PacketSHA256)
	require.Equal(t, "post-demux-000001.bin", record.PacketFile)
	require.Equal(t, int64(100), record.PTS)
	require.Equal(t, int64(99), record.DTS)
	require.Equal(t, int64(7), record.Duration)
	require.Equal(t, int64(11), record.Pos)
	require.Equal(t, "1/90000", record.TimeBase)
	require.Equal(t, 1, record.TimeBaseNum)
	require.Equal(t, 90000, record.TimeBaseDen)
	require.Equal(t, int(astiav.NewPacketFlags(astiav.PacketFlagKey)), record.Flags)
	require.True(t, record.KeyFrame)
	require.Equal(t, len(extraData), record.ExtraDataSize)
	require.Equal(t, sha256Hex(extraData), record.ExtraDataSHA256)
	require.Equal(t, hex.EncodeToString(extraData), record.ExtraDataHexPrefix)
	require.True(t, record.ExtraDataAV1CParses)
	require.NotNil(t, record.ExtraDataAV1C)
	require.Equal(t, 8, record.ExtraDataAV1C.BitDepth)

	packetBytes, err := os.ReadFile(filepath.Join(dir, record.PacketFile))
	require.NoError(t, err)
	require.Equal(t, payloads[0], packetBytes)
}

func TestDumpAV1PacketDoesNotOverwriteExistingFileAndRetainsSuccessBudget(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	dir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, dir)
	t.Setenv(av1PacketDumpMaxEnv, "1")

	existingPacketPath := filepath.Join(dir, "post-demux-000001.bin")
	existingRecordPath := filepath.Join(dir, "post-demux-000001.json")
	require.NoError(t, os.WriteFile(existingPacketPath, []byte("prior packet"), 0o600))
	require.NoError(t, os.WriteFile(existingRecordPath, []byte("prior record"), 0o600))

	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	require.False(t, dumpAV1DumpTestPacket(t, context.Background(), av1PacketDumpStagePostDemux, codecParams, []byte{0x01}, 100))

	priorPacket, err := os.ReadFile(existingPacketPath)
	require.NoError(t, err)
	require.Equal(t, []byte("prior packet"), priorPacket)
	priorRecord, err := os.ReadFile(existingRecordPath)
	require.NoError(t, err)
	require.Equal(t, []byte("prior record"), priorRecord)

	require.True(t, dumpAV1DumpTestPacket(t, context.Background(), av1PacketDumpStagePostDemux, codecParams, []byte{0x02}, 101))
	require.ElementsMatch(t, []string{
		"post-demux-000001.bin",
		"post-demux-000001.json",
		"post-demux-000002.bin",
		"post-demux-000002.json",
	}, readAV1DumpTestDir(t, dir))

	record := readAV1DumpTestRecord(t, filepath.Join(dir, "post-demux-000002.json"))
	require.Equal(t, uint64(2), record.Sequence)
	require.Equal(t, "post-demux-000002.bin", record.PacketFile)
}

func TestDumpAV1PacketDirectoryCreationFailureDoesNotConsumeSuccessBudget(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	parent := t.TempDir()
	blockerPath := filepath.Join(parent, "not-a-directory")
	require.NoError(t, os.WriteFile(blockerPath, []byte("blocker"), 0o600))

	t.Setenv(av1PacketDumpDirEnv, filepath.Join(blockerPath, "child"))
	t.Setenv(av1PacketDumpMaxEnv, "1")

	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	require.False(t, dumpAV1DumpTestPacket(t, context.Background(), av1PacketDumpStagePostDemux, codecParams, []byte{0x01}, 100))

	validDir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, validDir)
	require.True(t, dumpAV1DumpTestPacket(t, context.Background(), av1PacketDumpStagePostDemux, codecParams, []byte{0x02}, 101))
	require.ElementsMatch(t, []string{
		"post-demux-000001.bin",
		"post-demux-000001.json",
	}, readAV1DumpTestDir(t, validDir))
}

func TestDumpAV1PacketDirectoryCreationFailureIsBounded(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	ctx, hook := ctxWithQuietRecordingHook(t)
	parent := t.TempDir()
	blockerPath := filepath.Join(parent, "not-a-directory")
	require.NoError(t, os.WriteFile(blockerPath, []byte("blocker"), 0o600))

	t.Setenv(av1PacketDumpDirEnv, filepath.Join(blockerPath, "child"))
	t.Setenv(av1PacketDumpMaxEnv, "1")

	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	for idx := 0; idx < av1PacketDumpFailedAttemptMax+3; idx++ {
		require.False(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x01}, 100))
	}

	require.Equal(t, av1PacketDumpFailedAttemptMax, countAV1DumpTestWarningsContaining(hook, "unable to create AV1 packet dump directory"))

	require.NoError(t, os.Remove(blockerPath))
	require.False(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x02}, 101))
	_, err := os.Stat(filepath.Join(blockerPath, "child"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDumpAV1PacketWriteFailureIsBoundedAndTransientFailureCanRecover(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	ctx, hook := ctxWithQuietRecordingHook(t)
	dir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, dir)
	t.Setenv(av1PacketDumpMaxEnv, "1")

	for seq := uint64(1); seq <= av1PacketDumpFailedAttemptMax+1; seq++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "post-demux-"+zeroPadAV1PacketDumpSequence(seq)+".bin"),
			[]byte("prior packet"),
			0o600,
		))
	}

	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	for idx := 0; idx < av1PacketDumpFailedAttemptMax+2; idx++ {
		require.False(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x01}, 100))
	}

	require.Equal(t, av1PacketDumpFailedAttemptMax, countAV1DumpTestWarningsContaining(hook, "unable to write AV1 packet dump"))
	require.Equal(t, uint64(av1PacketDumpFailedAttemptMax), av1DumpTestSequenceCount(av1PacketDumpStagePostDemux))

	resetAV1PacketDumpStateForTest(t)
	ctx, _ = ctxWithQuietRecordingHook(t)
	recoverableDir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, recoverableDir)
	t.Setenv(av1PacketDumpMaxEnv, "1")
	require.NoError(t, os.WriteFile(filepath.Join(recoverableDir, "post-demux-000001.bin"), []byte("prior packet"), 0o600))

	require.False(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x02}, 101))
	require.True(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x03}, 102))
	require.ElementsMatch(t, []string{
		"post-demux-000001.bin",
		"post-demux-000002.bin",
		"post-demux-000002.json",
	}, readAV1DumpTestDir(t, recoverableDir))
}

func TestDumpAV1PacketInvalidMaxIsParsedOnce(t *testing.T) {
	resetAV1PacketDumpStateForTest(t)

	ctx, hook := ctxWithQuietRecordingHook(t)
	dir := t.TempDir()
	t.Setenv(av1PacketDumpDirEnv, dir)
	t.Setenv(av1PacketDumpMaxEnv, "invalid")

	codecParams := newAV1DumpTestCodecParameters(t, []byte{0x81, 0x00, 0x00, 0x00})
	defer codecParams.Free()

	require.True(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x01}, 100))
	require.True(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x02}, 101))

	t.Setenv(av1PacketDumpMaxEnv, "1")
	require.True(t, dumpAV1DumpTestPacket(t, ctx, av1PacketDumpStagePostDemux, codecParams, []byte{0x03}, 102))

	warnings := 0
	for _, entry := range hook.snapshot() {
		if entry.Level == beltlogger.LevelWarning && strings.Contains(entry.Message, "unable to parse "+av1PacketDumpMaxEnv) {
			warnings++
		}
	}
	require.Equal(t, 1, warnings)
}

func resetAV1PacketDumpStateForTest(t *testing.T) {
	t.Helper()

	av1PacketDumpMu.Lock()
	previousCounts := cloneAV1PacketDumpTestCounts(av1PacketDumpCounts)
	previousInFlightCounts := cloneAV1PacketDumpTestCounts(av1PacketDumpInFlightCounts)
	previousSequenceCounts := cloneAV1PacketDumpTestCounts(av1PacketDumpSequenceCounts)
	previousFailureCounts := cloneAV1PacketDumpTestFailureCounts(av1PacketDumpFailureCounts)
	previousFailureInFlightCounts := cloneAV1PacketDumpTestFailureCounts(av1PacketDumpFailureInFlightCounts)
	previousMaxParsed := av1PacketDumpMaxParsed
	previousMaxValue := av1PacketDumpMaxValue
	av1PacketDumpCounts = map[av1PacketDumpStage]uint64{}
	av1PacketDumpInFlightCounts = map[av1PacketDumpStage]uint64{}
	av1PacketDumpSequenceCounts = map[av1PacketDumpStage]uint64{}
	av1PacketDumpFailureCounts = map[av1PacketDumpFailureKey]uint64{}
	av1PacketDumpFailureInFlightCounts = map[av1PacketDumpFailureKey]uint64{}
	av1PacketDumpMaxParsed = false
	av1PacketDumpMaxValue = 0
	av1PacketDumpMu.Unlock()

	t.Cleanup(func() {
		av1PacketDumpMu.Lock()
		defer av1PacketDumpMu.Unlock()
		av1PacketDumpCounts = previousCounts
		av1PacketDumpInFlightCounts = previousInFlightCounts
		av1PacketDumpSequenceCounts = previousSequenceCounts
		av1PacketDumpFailureCounts = previousFailureCounts
		av1PacketDumpFailureInFlightCounts = previousFailureInFlightCounts
		av1PacketDumpMaxParsed = previousMaxParsed
		av1PacketDumpMaxValue = previousMaxValue
	})
}

func cloneAV1PacketDumpTestCounts(in map[av1PacketDumpStage]uint64) map[av1PacketDumpStage]uint64 {
	out := make(map[av1PacketDumpStage]uint64, len(in))
	for stage, count := range in {
		out[stage] = count
	}
	return out
}

func cloneAV1PacketDumpTestFailureCounts(in map[av1PacketDumpFailureKey]uint64) map[av1PacketDumpFailureKey]uint64 {
	out := make(map[av1PacketDumpFailureKey]uint64, len(in))
	for key, count := range in {
		out[key] = count
	}
	return out
}

func av1DumpTestSequenceCount(stage av1PacketDumpStage) uint64 {
	av1PacketDumpMu.Lock()
	defer av1PacketDumpMu.Unlock()

	return av1PacketDumpSequenceCounts[stage]
}

func newAV1DumpTestPacket(
	t *testing.T,
	payload []byte,
	pts int64,
) *astiav.Packet {
	t.Helper()

	pkt := astiav.AllocPacket()
	require.NotNil(t, pkt)
	require.NoError(t, pkt.FromData(payload))
	pkt.SetStreamIndex(3)
	pkt.SetPts(pts)
	pkt.SetDts(pts - 1)
	pkt.SetDuration(7)
	pkt.SetPos(11)
	pkt.SetFlags(astiav.NewPacketFlags(astiav.PacketFlagKey))
	return pkt
}

func newAV1DumpTestCodecParameters(
	t *testing.T,
	extraData []byte,
) *astiav.CodecParameters {
	t.Helper()

	codecParams := astiav.AllocCodecParameters()
	require.NotNil(t, codecParams)
	codecParams.SetMediaType(astiav.MediaTypeVideo)
	codecParams.SetCodecID(astiav.CodecIDAv1)
	require.NoError(t, codecParams.SetExtraData(extraData))
	return codecParams
}

func dumpAV1DumpTestPacket(
	t *testing.T,
	ctx context.Context,
	stage av1PacketDumpStage,
	codecParams *astiav.CodecParameters,
	payload []byte,
	pts int64,
) bool {
	t.Helper()

	pkt := newAV1DumpTestPacket(t, payload, pts)
	defer pkt.Free()
	return dumpAV1Packet(ctx, av1PacketDumpInput{
		Stage:           stage,
		Packet:          pkt,
		CodecParameters: codecParams,
		MediaType:       astiav.MediaTypeVideo,
		StreamIndex:     3,
		TimeBase:        astiav.NewRational(1, 90000),
	})
}

func readAV1DumpTestDir(
	t *testing.T,
	dir string,
) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func readAV1DumpTestRecord(
	t *testing.T,
	path string,
) av1PacketDumpRecord {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var record av1PacketDumpRecord
	require.NoError(t, json.Unmarshal(raw, &record))
	return record
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func countAV1DumpTestWarningsContaining(
	hook *quietRecordingHook,
	needle string,
) int {
	warnings := 0
	for _, entry := range hook.snapshot() {
		if entry.Level == beltlogger.LevelWarning && strings.Contains(entry.Message, needle) {
			warnings++
		}
	}
	return warnings
}
