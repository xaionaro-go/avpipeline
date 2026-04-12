//go:build test_e2e

package kernel_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	androidkernel "github.com/xaionaro-go/avpipeline/kernel/extra/android"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestAndroidMicrophoneRecordE2E(t *testing.T) {
	if runtime.GOOS != "android" {
		t.Skip("android-only test")
	}
	if env := os.Getenv("ANDROID_MIC_E2E"); env != "1" {
		t.Skip("set ANDROID_MIC_E2E=1 to run")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := androidkernel.MicrophoneConfig{
		SampleRate:   44100,
		Channels:     1,
		FrameSamples: 1024,
	}

	mic, err := androidkernel.NewMicrophone(ctx, cfg)
	require.NoError(t, err)
	defer mic.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- mic.Generate(ctx, outputCh)
	}()

	// Collect several frames and verify at least one contains non-zero
	// audio data. This catches the bug where Bytes()+copy was used
	// instead of SetBytes(), producing all-zero frames.
	const framesToCollect = 10
	var framesCollected int
	var hasNonZeroFrame bool
	for framesCollected < framesToCollect {
		select {
		case <-ctx.Done():
			require.FailNow(t, "timeout waiting for microphone output", ctx.Err().Error())
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				require.FailNow(t, "microphone failed", err.Error())
			}
		case out := <-outputCh:
			if out.Frame == nil {
				continue
			}
			framesCollected++

			// Check if the frame's PCM data contains any non-zero samples.
			pcm, err := out.Frame.Frame.Data().Bytes(0)
			require.NoError(t, err)
			for i := 0; i+1 < len(pcm); i += 2 {
				s := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
				if s != 0 {
					hasNonZeroFrame = true
					break
				}
			}
		}
	}
	require.True(t, hasNonZeroFrame,
		"all %d captured frames contained only zeros — audio data is not being written to frames",
		framesToCollect,
	)
}
