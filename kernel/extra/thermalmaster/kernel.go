package thermalmaster

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/thermalmaster/pkg/thermalmaster"
)

var _ kerneltypes.Abstract = (*Kernel)(nil)

// Kernel implements kernel/types.Abstract as a source-only kernel
// that reads frames from a ThermalMaster P3 thermal camera.
type Kernel struct {
	closeCh        chan struct{}
	config         atomic.Pointer[Config]
	device         *thermalmaster.Device
	legendRenderer *thermalmaster.LegendRenderer
}

// New creates a new Kernel with the given configuration.
func New(cfg Config) *Kernel {
	k := &Kernel{
		closeCh: make(chan struct{}),
	}
	k.config.Store(&cfg)
	return k
}

// String returns a human-readable name for this kernel.
func (k *Kernel) String() string {
	return "ThermalMasterP3"
}

// GetObjectID returns a unique identifier for this kernel instance.
func (k *Kernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

// CloseChan returns a channel that is closed when the kernel is closed.
func (k *Kernel) CloseChan() <-chan struct{} {
	return k.closeCh
}

// Close releases all resources held by the kernel.
func (k *Kernel) Close(_ context.Context) error {
	select {
	case <-k.closeCh:
		return nil
	default:
		close(k.closeCh)
	}
	if k.device != nil {
		return k.device.Close()
	}
	return nil
}

// SendInput is a no-op because this is a source-only kernel.
func (k *Kernel) SendInput(
	_ context.Context,
	_ packetorframe.InputUnion,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

// SetConfig atomically updates the kernel's configuration.
func (k *Kernel) SetConfig(cfg Config) {
	k.config.Store(&cfg)
}

// GetConfig returns the current configuration.
func (k *Kernel) GetConfig() Config {
	return *k.config.Load()
}

// Generate opens the P3 device, starts streaming, and continuously reads
// frames, converting each to an astiav.Frame and sending it to outputCh.
// It stops when ctx is cancelled or the kernel is closed.
func (k *Kernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	cfg := k.GetConfig()

	dev, err := thermalmaster.Open()
	if err != nil {
		return fmt.Errorf("opening device: %w", err)
	}
	k.device = dev
	defer func() {
		dev.Close()
		k.device = nil
	}()

	if err := dev.StartStreaming(ctx); err != nil {
		return fmt.Errorf("starting stream: %w", err)
	}
	defer dev.StopStreaming()

	modelCfg := dev.Config()

	outW, outH := modelCfg.SensorW, modelCfg.SensorH
	if cfg.Sensor == thermalmaster.SensorBlended {
		upCfg := thermalmaster.DefaultUpscaleConfig()
		if cfg.Upscale != nil {
			upCfg = *cfg.Upscale
		}
		outW *= upCfg.Factor
		outH *= upCfg.Factor
	}

	pixFmt := resolvePixelFormat(cfg)

	codecParams := astiav.AllocCodecParameters()
	defer codecParams.Free()
	codecParams.SetMediaType(astiav.MediaTypeVideo)
	codecParams.SetWidth(outW)
	codecParams.SetHeight(outH)
	codecParams.SetPixelFormat(pixFmt)

	timeBase := astiav.NewRational(1, 25)
	streamInfo := frame.BuildStreamInfo(
		nil,
		codecParams,
		0, 1,
		timeBase,
		0, nil,
	)

	var pts int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.closeCh:
			return nil
		default:
		}

		frameData, err := dev.ReadFrame(ctx)
		if err != nil {
			continue
		}

		cfg = k.GetConfig()

		fbCfg := thermalmaster.FrameBuilderConfig{
			Sensor:   cfg.Sensor,
			Colormap: cfg.Colormap,
			Upscale:  cfg.Upscale,
		}
		pixelBytes, _, _, _, thermal, ok := thermalmaster.BuildPixels(frameData, modelCfg, fbCfg)
		if !ok {
			continue
		}

		// Apply legend overlay if enabled.
		frameW, frameH := outW, outH
		if cfg.Legend != nil && cfg.Legend.Enabled && cfg.Colormap != nil && thermal != nil {
			if err := k.ensureLegendRenderer(cfg); err != nil {
				continue
			}

			tMin, tMax := thermalmaster.ThermalMinMax(thermal)
			result := k.legendRenderer.Apply(pixelBytes, thermalmaster.PixelFormatRGB24, outW, outH, tMin, tMax)
			if result != nil {
				frameW = result.Bounds().Dx()
				frameH = result.Bounds().Dy()
				pixelBytes = thermalmaster.RGBAToRGB24(result)
			}
		}

		f := astiav.AllocFrame()
		f.SetWidth(frameW)
		f.SetHeight(frameH)
		f.SetPixelFormat(pixFmt)
		if err := f.AllocBuffer(0); err != nil {
			f.Free()
			continue
		}

		if err := f.Data().SetBytes(pixelBytes, 1); err != nil {
			f.Free()
			continue
		}

		f.SetPts(pts)
		f.SetPktDts(pts)
		pts++

		output := frame.BuildOutput(f, streamInfo)

		select {
		case <-ctx.Done():
			f.Free()
			return ctx.Err()
		case <-k.closeCh:
			f.Free()
			return io.EOF
		case outputCh <- packetorframe.OutputUnion{Frame: &output}:
		}
	}
}

// resolvePixelFormat determines the output pixel format from the config.
// Sensor chooses the base data; Colormap decides whether to colorize.
func resolvePixelFormat(cfg Config) astiav.PixelFormat {
	if cfg.Colormap != nil {
		return astiav.PixelFormatRgb24
	}

	switch cfg.Sensor {
	case thermalmaster.SensorIR:
		return astiav.PixelFormatGray8
	default:
		return astiav.PixelFormatGray16Le
	}
}

func (k *Kernel) ensureLegendRenderer(cfg Config) error {
	if k.legendRenderer != nil {
		return nil
	}

	r, err := thermalmaster.NewLegendRenderer(*cfg.Legend)
	if err != nil {
		return fmt.Errorf("creating legend renderer: %w", err)
	}

	k.legendRenderer = r
	return nil
}
