//go:build android && cgo
// +build android,cgo

package android

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/AndroidGoLab/ndk/camera"
	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/observability"
)

func TestCamera2NDKGenerateCapturesFrame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	k, err := NewCamera2NDK(ctx, Camera2NDKConfig{})
	if camera2NDKE2EShouldSkip(err) {
		t.Skipf("camera2 ndk unavailable: %v", err)
	}
	require.NoError(t, err)

	outputCh := make(chan packetorframe.OutputUnion, 1)
	errCh := make(chan error, 1)
	observability.Go(ctx, func(ctx context.Context) {
		errCh <- k.Generate(ctx, outputCh)
	})
	allowCleanupSkip := false
	defer func() {
		err := k.Close(context.Background())
		if allowCleanupSkip && camera2NDKE2EShouldSkip(err) {
			t.Logf("camera2 ndk cleanup skipped after environment-limited capture: %v", err)
			return
		}
		require.NoError(t, err)
	}()

	var out packetorframe.OutputUnion
	select {
	case out = <-outputCh:
	case err := <-errCh:
		if camera2NDKE2EShouldSkip(err) {
			allowCleanupSkip = true
			t.Skipf("camera2 ndk unavailable: %v", err)
		}
		require.NoError(t, err)
	case <-ctx.Done():
		camera2NDKDumpGoroutines(t)
		if camera2NDKE2EShouldSkipNoFrame(ctx.Err()) {
			allowCleanupSkip = true
			t.Skipf("camera2 ndk produced no frame before deadline: %v", ctx.Err())
		}
		require.NoError(t, ctx.Err())
	}

	require.NotNil(t, out.Frame)
	require.NotNil(t, out.Frame.Frame)
	defer frame.Pool.Put(out.Frame.Frame)
	require.Equal(t, astiav.MediaTypeVideo, out.Frame.GetMediaType())
	require.Equal(t, camera2NDKDefaultWidth, int32(out.Frame.Frame.Width()))
	require.Equal(t, camera2NDKDefaultHeight, int32(out.Frame.Frame.Height()))
	require.Equal(t, astiav.PixelFormatYuv420P, out.Frame.Frame.PixelFormat())
	require.Positive(t, out.Frame.GetSize())
	require.GreaterOrEqual(t, out.Frame.GetPTS(), int64(0))

	require.NoError(t, k.Close(context.Background()))
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("camera2 ndk Generate did not return after Close")
	}
}

func TestCamera2NDKE2EStrictModeDisablesAllSkips(t *testing.T) {
	t.Setenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E", "1")

	for _, err := range []error{
		ErrCamera2NDKNoBackCamera,
		ErrCamera2NDKNoCompatibleBackCamera,
		ErrCamera2NDKSessionNotReady,
		ErrCamera2NDKSessionClosed,
		ErrCamera2NDKSessionCloseTimeout,
		camera.ErrPermissionDenied,
		errors.New("permission denied"),
	} {
		require.False(t, camera2NDKE2EShouldSkip(err), err)
	}
	require.False(t, camera2NDKE2EShouldSkip(nil))
}

func TestCamera2NDKE2ENonStrictModeSkipsExpectedEnvironmentErrors(t *testing.T) {
	t.Setenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E", "")

	require.True(t, camera2NDKE2EShouldSkip(ErrCamera2NDKNoBackCamera))
	require.True(t, camera2NDKE2EShouldSkip(ErrCamera2NDKSessionCloseTimeout))
	require.True(t, camera2NDKE2EShouldSkip(camera.ErrPermissionDenied))
	require.True(t, camera2NDKE2EShouldSkip(errors.New("permission denied")))
	require.False(t, camera2NDKE2EShouldSkip(errors.New("unexpected camera failure")))
}

func TestCamera2NDKE2EStrictModeRequiresFrameOnDeadline(t *testing.T) {
	t.Setenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E", "1")

	require.False(t, camera2NDKE2EShouldSkipNoFrame(context.DeadlineExceeded))
	require.False(t, camera2NDKE2EShouldSkipNoFrame(nil))
}

func TestCamera2NDKE2ENonStrictModeSkipsNoFrameDeadline(t *testing.T) {
	t.Setenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E", "")

	require.True(t, camera2NDKE2EShouldSkipNoFrame(context.DeadlineExceeded))
	require.False(t, camera2NDKE2EShouldSkipNoFrame(context.Canceled))
	require.False(t, camera2NDKE2EShouldSkipNoFrame(nil))
}

func camera2NDKDumpGoroutines(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&buf, 2))
	t.Logf("goroutines at camera2 ndk timeout:\n%s", buf.String())
}

func camera2NDKE2EShouldSkip(err error) bool {
	if err == nil {
		return false
	}
	// Native adb-shell emulator Camera2 can fail before session readiness; strict mode
	// keeps CI/device gates honest by requiring a real captured frame.
	if os.Getenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E") == "1" {
		return false
	}
	switch {
	case errors.Is(err, ErrCamera2NDKNoBackCamera),
		errors.Is(err, ErrCamera2NDKNoCompatibleBackCamera),
		errors.Is(err, ErrCamera2NDKSessionNotReady),
		errors.Is(err, ErrCamera2NDKSessionClosed),
		errors.Is(err, ErrCamera2NDKSessionCloseTimeout),
		errors.Is(err, camera.ErrPermissionDenied),
		errors.Is(err, camera.ErrCameraDisabled),
		errors.Is(err, camera.ErrCameraInUse),
		errors.Is(err, camera.ErrMaxCamerasInUse):
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "permission") ||
		strings.Contains(errText, "no camera") ||
		strings.Contains(errText, "camera disabled")
}

func camera2NDKE2EShouldSkipNoFrame(err error) bool {
	if os.Getenv("AVPIPELINE_CAMERA2NDK_STRICT_E2E") == "1" {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}
