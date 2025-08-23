package resampler

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/logger"
)

type Resampler struct {
	AudioFifo               *astiav.AudioFifo
	SoftwareResampleContext *astiav.SoftwareResampleContext
	FormatOutput            codec.PCMAudioFormat
	ResampledFrame          *astiav.Frame
}

func New(
	ctx context.Context,
	out codec.PCMAudioFormat,
) (_ret *Resampler, _err error) {
	logger.Tracef(ctx, "New: %+v", out)
	defer func() { logger.Tracef(ctx, "/New: %v %v", out, _ret, _err) }()

	fifo := astiav.AllocAudioFifo(
		out.SampleFormat,
		out.ChannelLayout.Channels(),
		out.ChunkSize,
	)
	if fifo == nil {
		return nil, fmt.Errorf("cannot alloc AudioFifo")
	}
	setFinalizerFree(ctx, fifo)

	swrCtx := astiav.AllocSoftwareResampleContext()
	if swrCtx == nil {
		return nil, fmt.Errorf("cannot alloc SoftwareResampleContext")
	}
	setFinalizerFree(ctx, swrCtx)

	resampledFrame := frame.Pool.Get()
	resampledFrame.SetNbSamples(out.ChunkSize)
	resampledFrame.SetChannelLayout(out.ChannelLayout)
	resampledFrame.SetSampleFormat(out.SampleFormat)
	resampledFrame.SetSampleRate(out.SampleRate)
	if err := resampledFrame.AllocBuffer(0); err != nil {
		return nil, fmt.Errorf("cannot alloc buffer for resampled frame: %w", err)
	}

	return &Resampler{
		AudioFifo:               fifo,
		SoftwareResampleContext: swrCtx,
		FormatOutput:            out,
		ResampledFrame:          resampledFrame,
	}, nil
}

func (r *Resampler) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()

	// all of that will be automatically freed by finalizers
	if r.AudioFifo != nil {
		r.AudioFifo = nil
	}
	if r.SoftwareResampleContext != nil {
		r.SoftwareResampleContext = nil
	}
	if r.ResampledFrame != nil {
		r.ResampledFrame = nil
	}

	return nil
}

func (r *Resampler) String() string {
	return fmt.Sprintf("Resampler<%s %dHz %s>",
		r.FormatOutput.SampleFormat, r.FormatOutput.SampleRate, r.FormatOutput.ChannelLayout,
	)
}

func (r *Resampler) AllocateOutputFrame(
	ctx context.Context,
) (_ret *astiav.Frame, _err error) {
	logger.Tracef(ctx, "AllocateOutputFrame")
	defer func() { logger.Tracef(ctx, "/AllocateOutputFrame: %v", _err) }()

	f := frame.Pool.Get()
	f.SetNbSamples(r.FormatOutput.ChunkSize)
	f.SetChannelLayout(r.FormatOutput.ChannelLayout)
	f.SetSampleFormat(r.FormatOutput.SampleFormat)
	f.SetSampleRate(r.FormatOutput.SampleRate)
	if err := f.AllocBuffer(0); err != nil {
		frame.Pool.Put(f)
		return nil, fmt.Errorf("cannot alloc buffer for output frame: %w", err)
	}
	return f, nil
}

func (r *Resampler) SendFrame(
	ctx context.Context,
	in *astiav.Frame,
) (_err error) {
	logger.Tracef(ctx, "SendFrame: %d", in.NbSamples())
	defer func() { logger.Tracef(ctx, "/SendFrame: %d: %v", in.NbSamples(), _err) }()

	if err := r.SoftwareResampleContext.ConvertFrame(in, r.ResampledFrame); err != nil {
		return fmt.Errorf("cannot convert frame: %w", err)
	}

	if nbSamples := r.ResampledFrame.NbSamples(); nbSamples == 0 {
		return nil
	}

	if _, err := r.AudioFifo.Write(r.ResampledFrame); err != nil {
		return fmt.Errorf("cannot write to AudioFifo: %w", err)
	}

	return nil
}

func (r *Resampler) receiveFrame(
	ctx context.Context,
	outputFrame *astiav.Frame,
	minSize int,
) (_err error) {
	logger.Tracef(ctx, "receiveFrames: %d", minSize)
	defer func() { logger.Tracef(ctx, "/receiveFrames: %d: %v", minSize, _err) }()

	if r.AudioFifo.Size() == 0 {
		return astiav.ErrEof
	}
	if r.AudioFifo.Size() < minSize {
		return astiav.ErrEagain
	}

	outputFrame.SetNbSamples(r.FormatOutput.ChunkSize)
	n, err := r.AudioFifo.Read(outputFrame)
	if err != nil {
		return fmt.Errorf("unable to read from AudioFifo: %w", err)
	}
	if n < minSize {
		logger.Errorf(ctx, "read less samples than requested: %d < %d", n, minSize)
	}
	outputFrame.SetNbSamples(n)
	return nil
}

func (r *Resampler) ReceiveFrame(
	ctx context.Context,
	frame *astiav.Frame,
) error {
	return r.receiveFrame(ctx, frame, r.FormatOutput.ChunkSize)
}

func (r *Resampler) Flush(
	ctx context.Context,
	frame *astiav.Frame,
) error {
	return r.receiveFrame(ctx, frame, 0)
}
