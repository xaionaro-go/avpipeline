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

	t.Run("autorotate_on_custom_option", func(t *testing.T) {
		// Same as autorotate_on but uses CustomOptions instead of typed fields,
		// exercising the PipelineSideData ordering fix in input.go.
		runRotationTestWithCustomOptions(t, ctx, inputFile, 16, 32, func(t *testing.T, f *astiav.Frame) {
			require.Equal(t, 16, f.Width())
			require.Equal(t, 32, f.Height())
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

func TestMidStreamRotation(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelDebug)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	tmpDir, err := os.MkdirTemp("", "avpipeline-test-midstream-rotation-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	inputFile := filepath.Join(tmpDir, "input.mov")

	// Create a longer 32x16 video (3 seconds) WITHOUT rotation metadata.
	// Left half is blue, right half is red.
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i",
		"color=c=red:size=32x16:d=3,drawbox=x=0:y=0:w=16:h=16:color=blue:t=fill",
		"-vcodec", "libx264", "-crf", "0", "-y", inputFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	t.Run("rotation_90_to_0", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		autoRotate := true
		displayRotation := 90.0
		inputConfig := kernel.InputConfig{
			AutoRotate:      &autoRotate,
			DisplayRotation: &displayRotation,
		}

		input, err := kernel.NewInputFromURL(ctx, inputFile, secret.New(""), inputConfig)
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

		// Wait for first frame — should be rotated (16x32).
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for first rotated frame")
		case f := <-collector.frames:
			require.Equal(t, 16, f.Width(), "first frame width should be 16 (rotated)")
			require.Equal(t, 32, f.Height(), "first frame height should be 32 (rotated)")
			f.Free()
		case err := <-errCh:
			t.Fatalf("pipeline error: %v", err)
		}

		// Change rotation mid-stream: remove rotation.
		input.SetDisplayRotation(ctx, 0)

		// Wait for a frame with updated dimensions (32x16).
		// The atomic update takes effect immediately, but a few frames
		// may already be in-flight through the decoder.
		deadline := time.After(10 * time.Second)
		foundUnrotated := false
		for !foundUnrotated {
			select {
			case <-deadline:
				t.Fatal("timeout waiting for unrotated frame")
			case f := <-collector.frames:
				if f.Width() == 32 && f.Height() == 16 {
					foundUnrotated = true
				}
				f.Free()
			case err := <-errCh:
				t.Fatalf("pipeline error: %v", err)
			}
		}
	})

	t.Run("rotation_90_to_180", func(t *testing.T) {
		// 180° rotation preserves dimensions (32x16 → 32x16) but flips content.
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		autoRotate := true
		displayRotation := 90.0
		inputConfig := kernel.InputConfig{
			AutoRotate:      &autoRotate,
			DisplayRotation: &displayRotation,
		}

		input, err := kernel.NewInputFromURL(ctx, inputFile, secret.New(""), inputConfig)
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

		// Wait for first frame — should be rotated 90° (16x32).
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for first rotated frame")
		case f := <-collector.frames:
			require.Equal(t, 16, f.Width())
			require.Equal(t, 32, f.Height())
			f.Free()
		case err := <-errCh:
			t.Fatalf("pipeline error: %v", err)
		}

		// Change rotation mid-stream to 180°: dimensions become 32x16.
		input.SetDisplayRotation(ctx, 180)

		deadline := time.After(10 * time.Second)
		found180 := false
		for !found180 {
			select {
			case <-deadline:
				t.Fatal("timeout waiting for 180° rotated frame")
			case f := <-collector.frames:
				if f.Width() == 32 && f.Height() == 16 {
					found180 = true
				}
				f.Free()
			case err := <-errCh:
				t.Fatalf("pipeline error: %v", err)
			}
		}
	})

	t.Run("same_angle_noop", func(t *testing.T) {
		// Setting the same rotation angle should not cause issues.
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		autoRotate := true
		displayRotation := 90.0
		inputConfig := kernel.InputConfig{
			AutoRotate:      &autoRotate,
			DisplayRotation: &displayRotation,
		}

		input, err := kernel.NewInputFromURL(ctx, inputFile, secret.New(""), inputConfig)
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

		// Wait for first frame — should be rotated (16x32).
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for first rotated frame")
		case f := <-collector.frames:
			require.Equal(t, 16, f.Width())
			require.Equal(t, 32, f.Height())
			f.Free()
		case err := <-errCh:
			t.Fatalf("pipeline error: %v", err)
		}

		// Set the same rotation angle — should be a no-op.
		input.SetDisplayRotation(ctx, 90)

		// Collect a few more frames; all should still be 16x32.
		for i := 0; i < 3; i++ {
			select {
			case <-ctx.Done():
				t.Fatal("timeout waiting for frame after same-angle set")
			case f := <-collector.frames:
				require.Equal(t, 16, f.Width(), "frame %d width after same-angle set", i)
				require.Equal(t, 32, f.Height(), "frame %d height after same-angle set", i)
				f.Free()
			case err := <-errCh:
				t.Fatalf("pipeline error: %v", err)
			}
		}
	})

	t.Run("rotation_0_to_270", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Start with NO rotation.
		autoRotate := true
		inputConfig := kernel.InputConfig{
			AutoRotate: &autoRotate,
		}

		input, err := kernel.NewInputFromURL(ctx, inputFile, secret.New(""), inputConfig)
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

		// Wait for first frame — should NOT be rotated (32x16).
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for first unrotated frame")
		case f := <-collector.frames:
			require.Equal(t, 32, f.Width(), "first frame width should be 32 (not rotated)")
			require.Equal(t, 16, f.Height(), "first frame height should be 16 (not rotated)")
			f.Free()
		case err := <-errCh:
			t.Fatalf("pipeline error: %v", err)
		}

		// Change rotation mid-stream: add 270° rotation.
		input.SetDisplayRotation(ctx, 270)

		// Wait for a rotated frame (16x32).
		deadline := time.After(10 * time.Second)
		foundRotated := false
		for !foundRotated {
			select {
			case <-deadline:
				t.Fatal("timeout waiting for rotated frame")
			case f := <-collector.frames:
				if f.Width() == 16 && f.Height() == 32 {
					foundRotated = true
				}
				f.Free()
			case err := <-errCh:
				t.Fatalf("pipeline error: %v", err)
			}
		}
	})
}

func runRotationTestWithCustomOptions(t *testing.T, ctx context.Context, inputFile string, expectedWidth, expectedHeight int, checkPixels func(*testing.T, *astiav.Frame)) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Use CustomOptions path instead of typed AutoRotate/DisplayRotation fields.
	// This exercises the PipelineSideData ordering fix where custom options
	// must be processed before PipelineSideData is populated.
	inputConfig := kernel.InputConfig{
		CustomOptions: globaltypes.DictionaryItems{
			{Key: "display_rotation", Value: "90"},
			{Key: "autorotate", Value: ""},
		},
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
		require.Equal(t, expectedWidth, f.Width())
		require.Equal(t, expectedHeight, f.Height())
		if checkPixels != nil {
			checkPixels(t, f)
		}
	case err := <-errCh:
		t.Fatalf("received error from pipeline: %v", err)
	}
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
