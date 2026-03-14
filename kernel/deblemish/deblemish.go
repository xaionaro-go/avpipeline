// Package deblemish provides a skin-smoothing (beauty filter) kernel using
// edge-preserving bilateral filtering. It supports multiple GPU backends
// (CUDA, OpenCL, Vulkan/libplacebo) and a CPU fallback, all configurable
// at runtime without restart.
package deblemish

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	uberatomic "go.uber.org/atomic"
)

// Deblemish is a kernel that applies edge-preserving bilateral filtering
// to video frames for skin smoothing. It supports whole-frame and
// face-region-only modes, with runtime-reconfigurable parameters.
type Deblemish struct {
	*closuresignaler.ClosureSignaler
	Enabled  *atomic.Bool       // nil = always on; false = passthrough
	SigmaS   uberatomic.Float64 // spatial sigma
	SigmaR   uberatomic.Float64 // range/color sigma
	Diameter uberatomic.Int64   // filter diameter (-1 = auto)
	FaceOnly uberatomic.Bool    // whole-frame vs face-region

	config          Config
	resolvedBackend Backend

	filterMu        sync.Mutex
	filterGraph     *kernel.AVFilterGraph
	lastSigmaS      float64
	lastSigmaR      float64
	lastDiameter    int
	lastFrameWidth  int
	lastFrameHeight int
	lastPixelFormat astiav.PixelFormat

	faceDetector faceDetectorState
}

var _ kernel.Abstract = (*Deblemish)(nil)

// New creates a new Deblemish kernel with the given configuration.
func New(cfg Config) (*Deblemish, error) {
	cfg.setDefaults()
	resolved := resolveBackend(cfg.Backend)

	d := &Deblemish{
		ClosureSignaler: closuresignaler.New(),
		config:          cfg,
		resolvedBackend: resolved,
		lastDiameter:    cfg.Diameter,
	}
	d.SigmaS.Store(cfg.SigmaS)
	d.SigmaR.Store(cfg.SigmaR)
	d.Diameter.Store(int64(cfg.Diameter))
	d.FaceOnly.Store(cfg.FaceOnly)

	if cfg.FaceOnly {
		if err := d.initFaceDetector(); err != nil {
			return nil, fmt.Errorf("unable to init face detector: %w", err)
		}
	}

	return d, nil
}

// ResolvedBackend returns the concrete backend in use.
func (d *Deblemish) ResolvedBackend() Backend {
	return d.resolvedBackend
}

func (d *Deblemish) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(d)
}

func (d *Deblemish) String() string {
	return fmt.Sprintf(
		"Deblemish(backend=%s,sigmaS=%g,sigmaR=%g)",
		d.resolvedBackend,
		d.SigmaS.Load(),
		d.SigmaR.Load(),
	)
}

func (d *Deblemish) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	if d.Enabled != nil && !d.Enabled.Load() {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	if frameInput.GetMediaType() != astiav.MediaTypeVideo {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	if d.FaceOnly.Load() {
		return d.sendInputFaceOnly(ctx, input, frameInput, outputCh)
	}

	return d.sendInputWholeFrame(ctx, input, frameInput, outputCh)
}

func (d *Deblemish) sendInputWholeFrame(
	ctx context.Context,
	input packetorframe.InputUnion,
	frameInput *frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	d.filterMu.Lock()
	defer d.filterMu.Unlock()

	sigmaS := d.SigmaS.Load()
	sigmaR := d.SigmaR.Load()
	diameter := int(d.Diameter.Load())

	needsRebuild := d.filterGraph == nil ||
		frameInput.Width() != d.lastFrameWidth ||
		frameInput.Height() != d.lastFrameHeight ||
		frameInput.PixelFormat() != d.lastPixelFormat

	// GPU backends require graph rebuild on parameter change;
	// CPU backend supports SendCommand instead.
	if !needsRebuild && d.resolvedBackend != BackendCPU {
		needsRebuild = sigmaS != d.lastSigmaS ||
			sigmaR != d.lastSigmaR ||
			diameter != d.lastDiameter
	}

	if needsRebuild {
		if err := d.rebuildFilterGraph(
			ctx,
			frameInput,
			sigmaS, sigmaR, diameter,
		); err != nil {
			return fmt.Errorf("unable to rebuild filter graph: %w", err)
		}
	}

	if d.resolvedBackend == BackendCPU {
		d.syncParamsCPU(ctx, sigmaS, sigmaR)
	}

	return d.filterGraph.SendInput(ctx, input, outputCh)
}

func (d *Deblemish) rebuildFilterGraph(
	ctx context.Context,
	frameInput *frame.Input,
	sigmaS, sigmaR float64,
	diameter int,
) error {
	if d.filterGraph != nil {
		if err := d.filterGraph.Close(ctx); err != nil {
			logger.Warnf(ctx, "deblemish: unable to close old filter graph: %v", err)
		}
		d.filterGraph = nil
	}

	streamIndex := frameInput.GetStreamIndex()
	filterStr := filterStringForBackend(d.resolvedBackend, sigmaS, sigmaR, diameter)
	filterComplex := fmt.Sprintf("[in%d]%s[out%d]", streamIndex, filterStr, streamIndex)

	trackConfig := map[int]avfilter.TrackConfig{
		streamIndex: {
			CodecParameters: frameInput.GetCodecParameters(),
			TimeBase:        frameInput.GetTimeBase(),
		},
	}

	graph, err := avfilter.NewGraph(ctx, trackConfig, filterComplex)
	if err != nil {
		return fmt.Errorf("unable to create bilateral filter graph: %w", err)
	}

	d.filterGraph = kernel.NewAVFilterGraph(ctx, graph)
	d.lastSigmaS = sigmaS
	d.lastSigmaR = sigmaR
	d.lastDiameter = diameter

	// Store actual frame dimensions (not codec parameters) for change detection,
	// because codec parameters may lag behind during mid-stream resolution changes.
	d.lastFrameWidth = frameInput.Width()
	d.lastFrameHeight = frameInput.Height()
	d.lastPixelFormat = frameInput.PixelFormat()

	logger.Debugf(
		ctx,
		"deblemish: built filter graph: backend=%s filter=%q",
		d.resolvedBackend, filterStr,
	)

	return nil
}

// syncParamsCPU uses SendCommand to update bilateral filter parameters
// at runtime without rebuilding the graph. Only works for the CPU backend
// whose sigmaS/sigmaR options have the T (timeline) flag.
func (d *Deblemish) syncParamsCPU(
	ctx context.Context,
	sigmaS, sigmaR float64,
) {
	if d.filterGraph == nil {
		return
	}

	graph, ok := d.filterGraph.Kernel.(*avfilter.Graph)
	if !ok {
		return
	}

	if sigmaS != d.lastSigmaS {
		_, err := graph.FilterGraph.SendCommand(
			"bilateral", "sigmaS", fmt.Sprintf("%g", sigmaS),
			astiav.NewFilterCommandFlags(),
		)
		if err != nil {
			logger.Warnf(ctx, "deblemish: SendCommand sigmaS failed: %v", err)
		} else {
			d.lastSigmaS = sigmaS
		}
	}

	if sigmaR != d.lastSigmaR {
		_, err := graph.FilterGraph.SendCommand(
			"bilateral", "sigmaR", fmt.Sprintf("%g", sigmaR),
			astiav.NewFilterCommandFlags(),
		)
		if err != nil {
			logger.Warnf(ctx, "deblemish: SendCommand sigmaR failed: %v", err)
		} else {
			d.lastSigmaR = sigmaR
		}
	}
}

func (d *Deblemish) Close(ctx context.Context) error {
	logger.Tracef(ctx, "Deblemish.Close")
	defer func() { logger.Tracef(ctx, "/Deblemish.Close") }()

	d.ClosureSignaler.Close(ctx)

	var errs []error

	d.filterMu.Lock()
	if d.filterGraph != nil {
		if err := d.filterGraph.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		d.filterGraph = nil
	}
	// closeFaceDetector is protected by filterMu to prevent a race when
	// Close() is called concurrently from multiple goroutines.
	if err := d.closeFaceDetector(); err != nil {
		errs = append(errs, err)
	}
	d.filterMu.Unlock()

	return errors.Join(errs...)
}

func (d *Deblemish) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}
