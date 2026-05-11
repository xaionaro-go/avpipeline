package avpipeline

import (
	"os"
	"strings"
	"testing"
)

func TestHotPathLogsAreNotDebug(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		file    string
		message string
	}{
		{
			file:    "node/node_serve.go",
			message: "nowhere to push to a %T",
		},
		{
			file:    "kernel/encoder.go",
			message: "encode-emit %s pts=%d key=%t",
		},
		{
			file:    "internal/set_finalizer.go",
			message: "freeing %T",
		},
		{
			file:    "codec/encoder_full_locked.go",
			message: "cannot set framerate from frame duration: frame has no duration",
		},
		{
			file:    "codec/encoder_full_locked.go",
			message: "waiting for more samples to stabilize framerate: curFPS:%v avgFPS:%f",
		},
		{
			file:    "kernel/output.go",
			message: "isKeyFrame:%t, expectedStreamsCount:%d, expectedStreamsVideoCount:%d, expectedStreamsAudioCount:%d, expectedStreamsSubtitleCount:%d, expectedStreamsDataCount:%d, videoBeforeAudio:%t",
		},
		{
			file:    "kernel/output.go",
			message: "not a key frame; skipping",
		},
		{
			file:    "kernel/output.go",
			message: "skipping a non-video (%s) packet to avoid MediaMTX from losing the video track",
		},
		{
			file:    "kernel/av1_packet_dump.go",
			message: "wrote AV1 packet dump stage=%s sequence=%d dir=%q",
		},
	} {
		t.Run(tc.file+"/"+tc.message, func(t *testing.T) {
			t.Parallel()

			srcBytes, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}

			src := string(srcBytes)
			forbidden := `logger.Debugf(ctx, "` + tc.message
			required := `logger.Tracef(ctx, "` + tc.message
			if strings.Contains(src, forbidden) {
				t.Fatalf("hot-path log %q in %s must not be Debugf", tc.message, tc.file)
			}
			if !strings.Contains(src, required) {
				t.Fatalf("hot-path log %q in %s must remain logged at Tracef", tc.message, tc.file)
			}
		})
	}
}
