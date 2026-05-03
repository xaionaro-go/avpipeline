package avfilter

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeVideoFrame(t *testing.T, w, h int) *astiav.Frame {
	t.Helper()
	f := astiav.AllocFrame()
	f.SetWidth(w)
	f.SetHeight(h)
	f.SetPixelFormat(astiav.PixelFormatYuv420P)
	require.NoError(t, f.AllocBuffer(32))
	return f
}

func TestNewFrameRotator_90(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	fr, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 90)
	require.NoError(t, err)
	defer fr.Close()

	out, err := fr.Rotate(ctx, f)
	require.NoError(t, err)
	testifyassert.Equal(t, 240, out.Width())
	testifyassert.Equal(t, 320, out.Height())
}

func TestNewFrameRotator_180(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	fr, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 180)
	require.NoError(t, err)
	defer fr.Close()

	out, err := fr.Rotate(ctx, f)
	require.NoError(t, err)
	testifyassert.Equal(t, 320, out.Width())
	testifyassert.Equal(t, 240, out.Height())
}

func TestNewFrameRotator_270(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	fr, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 270)
	require.NoError(t, err)
	defer fr.Close()

	out, err := fr.Rotate(ctx, f)
	require.NoError(t, err)
	testifyassert.Equal(t, 240, out.Width())
	testifyassert.Equal(t, 320, out.Height())
}

func TestNewFrameRotator_Negative90(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	fr, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), -90)
	require.NoError(t, err)
	defer fr.Close()

	out, err := fr.Rotate(ctx, f)
	require.NoError(t, err)
	testifyassert.Equal(t, 240, out.Width())
	testifyassert.Equal(t, 320, out.Height())
}

func TestNewFrameRotator_Zero(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	_, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 0)
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "no rotation needed")
}

func TestNewFrameRotator_Unsupported(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	_, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 45)
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "unsupported rotation angle")
}

func TestFrameRotator_CloseNil(t *testing.T) {
	var fr *FrameRotator
	fr.Close() // should not panic
}

func TestFrameRotator_MultipleFrames(t *testing.T) {
	ctx := context.Background()
	f := makeVideoFrame(t, 320, 240)
	defer f.Free()

	fr, err := NewFrameRotator(ctx, f, astiav.NewRational(1, 30), 90)
	require.NoError(t, err)
	defer fr.Close()

	for i := 0; i < 5; i++ {
		out, err := fr.Rotate(ctx, f)
		require.NoError(t, err)
		testifyassert.Equal(t, 240, out.Width())
		testifyassert.Equal(t, 320, out.Height())
	}
}
