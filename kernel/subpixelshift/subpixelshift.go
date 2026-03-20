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

	mu              xsync.Mutex
	ringBuffer      []*bufferedFrame
	head            int
	filled          int
	hrAccum         *hrGrid // luma accumulation grid
	hrChromaAccum   *hrGrid // chroma accumulation grid (half-res, 2 planes: U, V)
	lastWidth       int
	lastHeight      int
	rgbWarnedOnce   atomic.Bool
}

type bufferedFrame struct {
	planes []planeData // [0]=Y (full res), [1]=U (half res), [2]=V (half res)
	motion motionField
	width  int
	height int
	pts    int64
	dts    int64
	dur    int64
}

// yPlane returns the luma plane data, or nil if no planes are present.
func (bf *bufferedFrame) yPlane() [][]float64 {
	if len(bf.planes) == 0 {
		return nil
	}
	return bf.planes[0].data
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

	return k.processVideoFrame(ctx, frameInput, outputCh)
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
	outputCh chan<- packetorframe.OutputUnion,
) error {
	k.warnOnRGBMode(ctx)

	planes := extractPlanes(frameInput)
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
			planes: planes,
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
				resultErr = k.bilinearUpscaleFallback(ctx, bf, frameInput, scale, outputCh)
			case StartupModeBuffer:
				// No output yet; accumulating frames.
			default:
				resultErr = k.bilinearUpscaleFallback(ctx, bf, frameInput, scale, outputCh)
			}
			return
		}

		resultErr = k.performSR(ctx, scale, frameInput, outputCh)
	})
	return resultErr
}

// extractPlanes reads the Y, U, and V planes from a YUV420P frame as float64
// slices. It uses alignment=1 when reading bytes so linesizes equal the plane
// widths exactly. Returns nil if the frame is too small or unreadable.
func extractPlanes(f *frame.Input) []planeData {
	w := f.Width()
	h := f.Height()
	if w <= 0 || h <= 0 {
		return nil
	}

	chromaW := w / 2
	chromaH := h / 2

	ySize := w * h
	uSize := chromaW * chromaH
	vSize := chromaW * chromaH
	totalSize := ySize + uSize + vSize

	// Bytes(1) packs planes with alignment=1: Y then U then V contiguously.
	b, err := f.Data().Bytes(1)
	if err != nil || len(b) < totalSize {
		return nil
	}

	yData := bytesToFloat64Plane(b[:ySize], w, h)
	uData := bytesToFloat64Plane(b[ySize:ySize+uSize], chromaW, chromaH)
	vData := bytesToFloat64Plane(b[ySize+uSize:totalSize], chromaW, chromaH)

	return []planeData{
		{data: yData, width: w, height: h},
		{data: uData, width: chromaW, height: chromaH},
		{data: vData, width: chromaW, height: chromaH},
	}
}

// bytesToFloat64Plane converts a flat byte buffer into a 2D float64 slice.
func bytesToFloat64Plane(
	b []byte,
	width, height int,
) [][]float64 {
	plane := make([][]float64, height)
	for y := 0; y < height; y++ {
		row := make([]float64, width)
		rowStart := y * width
		for x := 0; x < width; x++ {
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
	k.hrChromaAccum = nil
}

// performSR orchestrates multi-frame super-resolution fusion for Y, U, and V.
// Motion estimation runs on the Y plane only. The same motion field is applied
// to chroma planes with displacements scaled by 0.5 (4:2:0 half-resolution).
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
	chromaHRW := (ref.width / 2) * scale
	chromaHRH := (ref.height / 2) * scale

	// Init or reset luma accumulation grid (1 plane).
	if k.hrAccum == nil || k.hrAccum.width != hrW || k.hrAccum.height != hrH {
		k.hrAccum = newHRGrid(hrW, hrH, 1)
	} else {
		k.hrAccum.reset()
	}

	// Init or reset chroma accumulation grid (2 planes: U, V).
	if k.hrChromaAccum == nil || k.hrChromaAccum.width != chromaHRW || k.hrChromaAccum.height != chromaHRH {
		k.hrChromaAccum = newHRGrid(chromaHRW, chromaHRH, 2)
	} else {
		k.hrChromaAccum.reset()
	}

	// Fuse each buffered frame.
	for i := 0; i < k.filled; i++ {
		idx := k.bufferIndex(i, bufSize)
		bf := k.ringBuffer[idx]
		if bf == nil || len(bf.planes) == 0 {
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

		// Fuse luma (Y) plane.
		fuseFrame(k.hrAccum, 0, bf.planes[0].data, mf, scale, age, confidence)

		// Fuse chroma (U, V) planes with halved motion for 4:2:0.
		if len(bf.planes) >= 3 {
			chromaMF := mf.scaled(0.5)
			fuseFrame(k.hrChromaAccum, 0, bf.planes[1].data, chromaMF, scale, age, confidence)
			fuseFrame(k.hrChromaAccum, 1, bf.planes[2].data, chromaMF, scale, age, confidence)
		}
	}

	hrY := extractHRPlane(&k.hrAccum.planes[0], hrW, hrH, 0.001)
	hrU := extractHRPlane(&k.hrChromaAccum.planes[0], chromaHRW, chromaHRH, 0.001)
	hrV := extractHRPlane(&k.hrChromaAccum.planes[1], chromaHRW, chromaHRH, 0.001)

	return k.buildOutputFrame(ctx, hrY, hrU, hrV, hrW, hrH, ref, refInput, outputCh)
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
	refY := ref.yPlane()
	curY := cur.yPlane()
	if refY == nil || curY == nil {
		return zeroMotionField()
	}
	mf, err := estimateGlobalMotion(refY, curY)
	if err != nil {
		return zeroMotionField()
	}
	return mf
}

// buildOutputFrame constructs the output astiav.Frame from the HR Y, U, and V planes.
func (k *Kernel) buildOutputFrame(
	ctx context.Context,
	hrY, hrU, hrV [][]float64,
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

	// Use alignment=1 for SetBytes so linesize == width for Y, width/2 for chroma.
	chromaH := hrH / 2
	chromaW := hrW / 2

	ySize := hrW * hrH
	uSize := chromaW * chromaH
	vSize := chromaW * chromaH
	totalSize := ySize + uSize + vSize

	buf := make([]byte, totalSize)

	// Y plane.
	writePlaneToBuffer(buf[:ySize], hrY, hrW, hrH)

	// U plane.
	writePlaneToBuffer(buf[ySize:ySize+uSize], hrU, chromaW, chromaH)

	// V plane.
	writePlaneToBuffer(buf[ySize+uSize:totalSize], hrV, chromaW, chromaH)

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

// writePlaneToBuffer writes a float64 2D plane into a byte buffer, clamping
// values to [0, 255]. If the plane is nil or undersized, fills with 128
// (neutral chroma / mid-gray luma).
func writePlaneToBuffer(
	buf []byte,
	plane [][]float64,
	width, height int,
) {
	if len(plane) < height {
		for i := range buf {
			buf[i] = 128
		}
		return
	}

	for y := 0; y < height; y++ {
		rowOff := y * width
		row := plane[y]
		for x := 0; x < width; x++ {
			buf[rowOff+x] = byte(math.Round(clampFloat(row[x], 0, 255)))
		}
	}
}

// bilinearUpscaleFallback produces a single-frame upscale using bilinear
// interpolation on all planes (startup fallback before enough frames accumulate).
func (k *Kernel) bilinearUpscaleFallback(
	ctx context.Context,
	bf *bufferedFrame,
	refInput *frame.Input,
	scale int,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if len(bf.planes) == 0 {
		return k.passthrough(ctx,
			packetorframe.InputUnion{Frame: refInput},
			outputCh,
		)
	}

	hrW := bf.width * scale
	hrH := bf.height * scale

	hrY := bilinearUpscalePlane(bf.planes[0].data, hrW, hrH, scale)

	chromaHRW := (bf.width / 2) * scale
	chromaHRH := (bf.height / 2) * scale

	var hrU, hrV [][]float64
	if len(bf.planes) >= 3 {
		hrU = bilinearUpscalePlane(bf.planes[1].data, chromaHRW, chromaHRH, scale)
		hrV = bilinearUpscalePlane(bf.planes[2].data, chromaHRW, chromaHRH, scale)
	}

	return k.buildOutputFrame(ctx, hrY, hrU, hrV, hrW, hrH, bf, refInput, outputCh)
}

// bilinearUpscalePlane upscales a single plane by the given scale factor
// using bilinear interpolation.
func bilinearUpscalePlane(
	plane [][]float64,
	hrW, hrH int,
	scale int,
) [][]float64 {
	hr := make2D(hrH, hrW)
	scaleF := float64(scale)

	for hy := 0; hy < hrH; hy++ {
		for hx := 0; hx < hrW; hx++ {
			lx := float64(hx) / scaleF
			ly := float64(hy) / scaleF
			hr[hy][hx] = bilinearSample(plane, lx, ly)
		}
	}
	return hr
}

// warnOnRGBMode logs a one-time warning when ColorModeRGB is set, since RGB
// processing is not yet implemented and falls back to YUV processing.
func (k *Kernel) warnOnRGBMode(ctx context.Context) {
	if ColorMode(k.ColorMode.Load()) != ColorModeRGB {
		return
	}
	if k.rgbWarnedOnce.Load() {
		return
	}
	k.rgbWarnedOnce.Store(true)
	logger.Warnf(ctx, "ColorModeRGB is not yet implemented; falling back to YUV processing")
}

func ptrFrameOutput(o frame.Output) *frame.Output {
	return &o
}
