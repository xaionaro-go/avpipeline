package kernel_test

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

type frameCollector struct {
	closuresignaler.ClosureSignaler
	frames chan *astiav.Frame
}

func newFrameCollector() *frameCollector {
	return &frameCollector{
		ClosureSignaler: *closuresignaler.New(),
		frames:          make(chan *astiav.Frame, 10),
	}
}

func (c *frameCollector) SendInput(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
	if input.Frame != nil && input.Frame.Frame != nil {
		f := astiav.AllocFrame()
		f.Ref(input.Frame.Frame)
		c.frames <- f
	}
	return nil
}

func (c *frameCollector) String() string                    { return "frameCollector" }
func (c *frameCollector) GetObjectID() globaltypes.ObjectID { return globaltypes.GetObjectID(c) }
func (c *frameCollector) Close(ctx context.Context) error   { return nil }
func (c *frameCollector) CloseChan() <-chan struct{}        { return c.ClosureSignaler.CloseChan() }
func (c *frameCollector) Generate(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
	return nil
}

func TestAutoRotate(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelDebug)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	tmpDir, err := os.MkdirTemp("", "avpipeline-test-rotation-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	inputFile := filepath.Join(tmpDir, "input.mov")

	// Create a 32x16 video with 90-degree rotation metadata
	// Left half is blue, right half is red
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "color=c=red:size=32x16:d=1,drawbox=x=0:y=0:w=16:h=16:color=blue:t=fill", "-vcodec", "libx264", "-crf", "0", "-metadata:s:v", "rotation=-90", "-y", inputFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	t.Run("autorotate_on", func(t *testing.T) {
		// Output should be rotated 90 deg clockwise: 16x32
		runRotationTest(t, ctx, inputFile, true, 16, 32, func(t *testing.T, f *astiav.Frame) {
			require.Equal(t, 16, f.Width())
			require.Equal(t, 32, f.Height())

			img, err := f.Data().GuessImageFormat()
			require.NoError(t, err)
			err = f.Data().ToImage(img)
			require.NoError(t, err)

			ycbcr, ok := img.(*image.YCbCr)
			require.True(t, ok, "Expected YCbCr image")

			// 90 CW rotation of (Left Blue, Right Red) 32x16 -> 16x32
			// The new Top half (y < 16) should be Blue.
			// The new Bottom half (y >= 16) should be Red.
			// Red Y ~ 80, Blue Y ~ 40
			require.Less(t, ycbcr.Y[ycbcr.YOffset(0, 0)], uint8(60), "Pixel (0,0) should be Blue")
			require.Greater(t, ycbcr.Y[ycbcr.YOffset(0, 31)], uint8(60), "Pixel (0,31) should be Red")
		})
	})

	t.Run("autorotate_off", func(t *testing.T) {
		// Output should NOT be rotated: 32x16
		runRotationTest(t, ctx, inputFile, false, 32, 16, func(t *testing.T, f *astiav.Frame) {
			require.Equal(t, 32, f.Width())
			require.Equal(t, 16, f.Height())

			img, err := f.Data().GuessImageFormat()
			require.NoError(t, err)
			err = f.Data().ToImage(img)
			require.NoError(t, err)

			ycbcr, ok := img.(*image.YCbCr)
			require.True(t, ok, "Expected YCbCr image")

			// Left Blue, Right Red
			require.Less(t, ycbcr.Y[ycbcr.YOffset(0, 0)], uint8(60), "Pixel (0,0) should be Blue")
			require.Greater(t, ycbcr.Y[ycbcr.YOffset(31, 0)], uint8(60), "Pixel (31,0) should be Red")
		})
	})
}

func runRotationTest(t *testing.T, ctx context.Context, inputFile string, autoRotate bool, expectedWidth, expectedHeight int, checkPixels func(*testing.T, *astiav.Frame)) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	inputConfig := kernel.InputConfig{
		AutoRotate: &autoRotate,
	}
	if autoRotate {
		r := 90.0
		inputConfig.DisplayRotation = &r
	}

	input, err := kernel.NewInputFromURL(
		ctx,
		inputFile, secret.New(""),
		inputConfig,
	)
	require.NoError(t, err)
	defer input.Close(ctx)

	decoder := kernel.NewDecoder(ctx, &codec.NaiveDecoderFactory{})
	defer decoder.Close(ctx)

	collector := newFrameCollector()

	inputNode := node.NewFromKernel(ctx, input)
	decoderNode := node.NewFromKernel(ctx, decoder)
	collectorNode := node.NewFromKernel(ctx, collector)

	inputNode.AddPushTo(ctx, decoderNode)
	decoderNode.AddPushTo(ctx, collectorNode)

	errCh := make(chan node.Error, 10)
	go avpipeline.Serve(ctx, avpipeline.ServeConfig{}, errCh, inputNode)

	select {
	case <-ctx.Done():
		t.Fatal("timeout waiting for frame")
	case f := <-collector.frames:
		defer f.Free()
		fmt.Printf("Received frame: %dx%d\n", f.Width(), f.Height())
		require.Equal(t, expectedWidth, f.Width())
		require.Equal(t, expectedHeight, f.Height())
		if checkPixels != nil {
			checkPixels(t, f)
		}
	case err := <-errCh:
		t.Fatalf("received error from pipeline: %v", err)
	}
}
