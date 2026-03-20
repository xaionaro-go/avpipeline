package subpixelshift

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"

	astiav "github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
	uberatomic "go.uber.org/atomic"
)

// Kernel implements sub-pixel shift super-resolution upscaling. It accumulates
// a ring buffer of low-resolution frames, estimates inter-frame motion, and
// fuses the observations into a higher-resolution output.
type Kernel struct {
	*closuresignaler.ClosureSignaler

	Scale       uberatomic.Int32
	BufferSize  uberatomic.Int32
	Enabled     *atomic.Bool // nil = always on
	ColorMode   uberatomic.Int32
	MotionMode  uberatomic.Int32
	BlockSize   uberatomic.Int32
	StartupMode uberatomic.Int32

	mu         xsync.Mutex
	ringBuffer []*bufferedFrame
	head       int
	filled     int
	hrAccum    *hrGrid
	lastWidth  int
	lastHeight int
}

type bufferedFrame struct {
	planes [][]float64
	motion motionField
	width  int
	height int
	pts    int64
	dts    int64
	dur    int64
}

var _ kerneltypes.Abstract = (*Kernel)(nil)

// New creates a Kernel from the given options, applying defaults for unset fields.
func New(opts ...Option) *Kernel {
	cfg := Options(opts).Config()

	k := &Kernel{
		ClosureSignaler: closuresignaler.New(),
		ringBuffer:      make([]*bufferedFrame, cfg.BufferSize),
	}
	k.Scale.Store(cfg.Scale)
	k.BufferSize.Store(cfg.BufferSize)
	k.ColorMode.Store(int32(cfg.ColorMode))
	k.MotionMode.Store(int32(cfg.MotionMode))
	k.BlockSize.Store(cfg.BlockSize)
	k.StartupMode.Store(int32(cfg.StartupMode))
	return k
}

func (k *Kernel) String() string {
	return fmt.Sprintf(
		"SubPixelShift(scale=%d, buf=%d)",
		k.Scale.Load(),
		k.BufferSize.Load(),
	)
}

func (k *Kernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Kernel) Close(ctx context.Context) error {
	k.ClosureSignaler.Close(ctx)
	return nil
}

func (k *Kernel) CloseChan() <-chan struct{} {
	return k.ClosureSignaler.CloseChan()
}

func (k *Kernel) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (k *Kernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()

	_, frameInput := input.Unwrap()
	if frameInput == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	// Audio frames pass through unchanged.
	if frameInput.GetMediaType() != astiav.MediaTypeVideo {
		return k.passthrough(ctx, input, outputCh)
	}

	// Disabled → passthrough.
	if k.Enabled != nil && !k.Enabled.Load() {
		return k.passthrough(ctx, input, outputCh)
	}

	return k.processVideoFrame(ctx, frameInput, input, outputCh)
}

func (k *Kernel) passthrough(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	output := input.CloneAsReferencedOutput()
	if output.Get() == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputCh <- output:
	}
	return nil
}

func (k *Kernel) processVideoFrame(
	ctx context.Context,
	frameInput *frame.Input,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	yPlane := extractYPlane(frameInput)
	w := frameInput.Width()
	h := frameInput.Height()

	codecMF, _ := extractCodecMotionField(frameInput.Frame)

	scale := int(k.Scale.Load())
	bufSize := int(k.BufferSize.Load())
	startupMode := StartupMode(k.StartupMode.Load())

	var resultErr error
	k.mu.Do(ctx, func() {
		// Handle resolution change: flush the ring buffer.
		if w != k.lastWidth || h != k.lastHeight {
			k.flushBuffer()
			k.lastWidth = w
			k.lastHeight = h
		}

		bf := &bufferedFrame{
			planes: yPlane,
			motion: codecMF,
			width:  w,
			height: h,
			pts:    frameInput.GetPTS(),
			dts:    frameInput.GetDTS(),
			dur:    frameInput.GetDuration(),
		}

		k.ringBuffer[k.head] = bf
		k.head = (k.head + 1) % bufSize
		if k.filled < bufSize {
			k.filled++
		}

		halfBuf := bufSize / 2
		if halfBuf < 1 {
			halfBuf = 1
		}

		if k.filled < halfBuf {
			switch startupMode {
			case StartupModePassthrough:
				resultErr = k.bicubicUpscale(ctx, bf, frameInput, scale, outputCh)
			case StartupModeBuffer:
				// No output yet; accumulating frames.
			default:
				resultErr = k.bicubicUpscale(ctx, bf, frameInput, scale, outputCh)
			}
			return
		}

		resultErr = k.performSR(ctx, scale, frameInput, outputCh)
	})
	return resultErr
}

// extractYPlane reads the Y (luma) plane from the frame as a 2D float64 slice.
func extractYPlane(f *frame.Input) [][]float64 {
	w := f.Width()
	h := f.Height()
	linesize := f.Linesize()
	yLinesize := linesize[0]

	if yLinesize <= 0 || w <= 0 || h <= 0 {
		return nil
	}

	// Read the raw image bytes with alignment=1 to get actual pixel data.
	b, err := f.Data().Bytes(1)
	if err != nil || len(b) < h*yLinesize {
		return nil
	}

	plane := make([][]float64, h)
	for y := 0; y < h; y++ {
		row := make([]float64, w)
		rowStart := y * yLinesize
		for x := 0; x < w; x++ {
			row[x] = float64(b[rowStart+x])
		}
		plane[y] = row
	}
	return plane
}

func (k *Kernel) flushBuffer() {
	for i := range k.ringBuffer {
		k.ringBuffer[i] = nil
	}
	k.head = 0
	k.filled = 0
	k.hrAccum = nil
}

// performSR orchestrates multi-frame super-resolution fusion.
func (k *Kernel) performSR(
	ctx context.Context,
	scale int,
	refInput *frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	bufSize := int(k.BufferSize.Load())
	motionMode := MotionMode(k.MotionMode.Load())

	// The reference is the center of the filled portion.
	refIdx := k.referenceIndex(bufSize)
	ref := k.ringBuffer[refIdx]
	if ref == nil {
		return nil
	}

	hrW := ref.width * scale
	hrH := ref.height * scale

	// Init or reset accumulation grid.
	if k.hrAccum == nil || k.hrAccum.width != hrW || k.hrAccum.height != hrH {
		k.hrAccum = newHRGrid(hrW, hrH, 1)
	} else {
		k.hrAccum.reset()
	}

	// Fuse each buffered frame.
	for i := 0; i < k.filled; i++ {
		idx := k.bufferIndex(i, bufSize)
		bf := k.ringBuffer[idx]
		if bf == nil || bf.planes == nil {
			continue
		}

		age := i - k.filledCenterOffset()
		var mf motionField
		if idx == refIdx {
			mf = zeroMotionField()
			mf.confidence = 1.0
		} else {
			mf = k.estimateMotion(ref, bf, motionMode)
		}

		if isExcessiveMotion(mf, ref.width, ref.height) {
			continue
		}

		confidence := mf.confidence
		if confidence <= 0 {
			confidence = 0.1
		}

		fuseFrame(k.hrAccum, 0, bf.planes, mf, scale, age, confidence)
	}

	hrPlane := extractHRPlane(&k.hrAccum.planes[0], hrW, hrH, 0.001)
	return k.buildOutputFrame(ctx, hrPlane, hrW, hrH, ref, refInput, outputCh)
}

// referenceIndex returns the ring buffer index of the center (reference) frame.
func (k *Kernel) referenceIndex(bufSize int) int {
	centerOffset := k.filledCenterOffset()
	// Oldest frame is at index (head - filled) mod bufSize.
	oldest := ((k.head - k.filled) % bufSize + bufSize) % bufSize
	return (oldest + centerOffset) % bufSize
}

// filledCenterOffset returns the offset within the filled portion for the center frame.
func (k *Kernel) filledCenterOffset() int {
	return k.filled / 2
}

// bufferIndex returns the ring buffer index for the i-th oldest frame.
func (k *Kernel) bufferIndex(i, bufSize int) int {
	oldest := ((k.head - k.filled) % bufSize + bufSize) % bufSize
	return (oldest + i) % bufSize
}

func (k *Kernel) estimateMotion(
	ref, cur *bufferedFrame,
	mode MotionMode,
) motionField {
	switch mode {
	case MotionModeCodecMVs:
		return cur.motion
	case MotionModeGlobal:
		return k.phaseCorrelation(ref, cur)
	case MotionModeAuto:
		if cur.motion.confidence > 0 {
			return cur.motion
		}
		return k.phaseCorrelation(ref, cur)
	default:
		return zeroMotionField()
	}
}

func (k *Kernel) phaseCorrelation(
	ref, cur *bufferedFrame,
) motionField {
	if ref.planes == nil || cur.planes == nil {
		return zeroMotionField()
	}
	mf, err := estimateGlobalMotion(ref.planes, cur.planes)
	if err != nil {
		return zeroMotionField()
	}
	return mf
}

// buildOutputFrame constructs the output astiav.Frame from the HR luma plane.
func (k *Kernel) buildOutputFrame(
	ctx context.Context,
	hrPlane [][]float64,
	hrW, hrH int,
	ref *bufferedFrame,
	refInput *frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	outFrame := astiav.AllocFrame()
	outFrame.SetWidth(hrW)
	outFrame.SetHeight(hrH)
	outFrame.SetPixelFormat(astiav.PixelFormatYuv420P)
	if err := outFrame.AllocBuffer(0); err != nil {
		outFrame.Free()
		return fmt.Errorf("allocating output frame buffer: %w", err)
	}

	if err := outFrame.MakeWritable(); err != nil {
		outFrame.Free()
		return fmt.Errorf("making output frame writable: %w", err)
	}

	linesize := outFrame.Linesize()
	yLinesize := linesize[0]
	uLinesize := linesize[1]
	vLinesize := linesize[2]

	// Build output image bytes: Y from hrPlane, U/V=128 (neutral chroma).
	chromaH := hrH / 2
	chromaW := hrW / 2

	ySize := yLinesize * hrH
	uSize := uLinesize * chromaH
	vSize := vLinesize * chromaH
	totalSize := ySize + uSize + vSize

	buf := make([]byte, totalSize)

	// Y plane.
	for y := 0; y < hrH; y++ {
		rowOff := y * yLinesize
		for x := 0; x < hrW; x++ {
			v := math.Round(clampFloat(hrPlane[y][x], 0, 255))
			buf[rowOff+x] = byte(v)
		}
	}

	// U plane: fill with 128.
	uOff := ySize
	for y := 0; y < chromaH; y++ {
		rowOff := uOff + y*uLinesize
		for x := 0; x < chromaW; x++ {
			buf[rowOff+x] = 128
		}
	}

	// V plane: fill with 128.
	vOff := ySize + uSize
	for y := 0; y < chromaH; y++ {
		rowOff := vOff + y*vLinesize
		for x := 0; x < chromaW; x++ {
			buf[rowOff+x] = 128
		}
	}

	if err := outFrame.Data().SetBytes(buf, 1); err != nil {
		outFrame.Free()
		return fmt.Errorf("writing output frame data: %w", err)
	}

	outFrame.SetPts(ref.pts)
	outFrame.SetPktDts(ref.dts)
	outFrame.SetDuration(ref.dur)

	output := packetorframe.OutputUnion{
		Frame: ptrFrameOutput(frame.BuildOutput(outFrame, refInput.StreamInfo)),
	}

	select {
	case <-ctx.Done():
		outFrame.Free()
		return ctx.Err()
	case outputCh <- output:
	}
	return nil
}

// bicubicUpscale produces a single-frame upscale using bilinear interpolation
// on the Y plane (startup fallback).
func (k *Kernel) bicubicUpscale(
	ctx context.Context,
	bf *bufferedFrame,
	refInput *frame.Input,
	scale int,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if bf.planes == nil {
		return k.passthrough(ctx,
			packetorframe.InputUnion{Frame: refInput},
			outputCh,
		)
	}

	hrW := bf.width * scale
	hrH := bf.height * scale
	hrPlane := make2D(hrH, hrW)
	scaleF := float64(scale)

	for hy := 0; hy < hrH; hy++ {
		for hx := 0; hx < hrW; hx++ {
			lx := float64(hx) / scaleF
			ly := float64(hy) / scaleF
			hrPlane[hy][hx] = bilinearSample(bf.planes, lx, ly)
		}
	}

	return k.buildOutputFrame(ctx, hrPlane, hrW, hrH, bf, refInput, outputCh)
}

func ptrFrameOutput(o frame.Output) *frame.Output {
	return &o
}
