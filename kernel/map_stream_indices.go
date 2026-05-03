// map_stream_indices.go implements a kernel that maps input stream indices to output stream indices.

package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type MapStreamIndices struct {
	*closuresignaler.ClosureSignaler
	Locker    xsync.Mutex
	StreamMap map[InternalStreamKey][]int
	Assigner  StreamIndexAssigner

	formatContext *astiav.FormatContext
	outputStreams map[int]*astiav.Stream

	// upstreamSource captures the most recent upstream source observed by
	// sendInput. Encoder construction downstream type-asserts the frame
	// source to codec.GetDecoderer in order to bind the encoder to the
	// upstream decoder's MediaCodec resources (surface passthrough,
	// hardware-frame reuse). MapStreamIndices replaces the source on
	// outgoing units with itself, hiding the upstream decoder; storing
	// the upstream source here lets MapStreamIndices forward GetDecoder()
	// so the encoder still sees the original decoder.
	upstreamSource packetorframe.AbstractSource
}

var (
	_ Abstract           = (*MapStreamIndices)(nil)
	_ packet.Source      = (*MapStreamIndices)(nil)
	_ packet.Sink        = (*MapStreamIndices)(nil)
	_ codec.GetDecoderer = (*MapStreamIndices)(nil)
)

type StreamIndexAssigner interface {
	StreamIndexAssign(context.Context, packetorframe.InputUnion) ([]int, error)
}

func NewMapStreamIndices(
	ctx context.Context,
	assigner StreamIndexAssigner,
) *MapStreamIndices {
	m := &MapStreamIndices{
		ClosureSignaler: closuresignaler.New(),
		StreamMap:       make(map[InternalStreamKey][]int),
		Assigner:        assigner,

		formatContext: astiav.AllocFormatContext(),
		outputStreams: make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, m.formatContext)
	return m
}

func (m *MapStreamIndices) getOutputStreamIndexes(
	ctx context.Context,
	input packetorframe.InputUnion,
) (_ret []int, _err error) {
	streamKey := InternalStreamKey{
		StreamIndex: input.GetStreamIndex(),
		Source:      input.GetSource(),
	}
	if v, ok := m.StreamMap[streamKey]; ok {
		return v, nil
	}

	defer func() {
		if _err != nil {
			return
		}
		logger.Debugf(ctx, "assigning indexes for %#+v: %v", streamKey, _ret)
		m.StreamMap[streamKey] = _ret
	}()

	if m.Assigner == nil {
		return []int{input.GetStreamIndex()}, nil
	}

	return m.Assigner.StreamIndexAssign(ctx, input)
}

func (m *MapStreamIndices) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return xsync.DoA3R1(
		ctx,
		&m.Locker,
		m.sendInput,
		ctx, input, outputCh,
	)
}

func (m *MapStreamIndices) getOutputStreamByIndex(
	ctx context.Context,
	outputStreamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := m.outputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	outputStream, err := m.newOutputStream(
		ctx,
		outputStreamIndex,
		codecParameters, timeBase,
	)
	if err != nil {
		return nil, err
	}
	m.outputStreams[outputStreamIndex] = outputStream
	return outputStream, nil
}

func (m *MapStreamIndices) newOutputStream(
	ctx context.Context,
	outputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	assert(ctx, codecParams != nil, "codecParams is nil")
	assert(ctx, m.formatContext != nil, "formatContext is nil")
	outputStream := m.formatContext.NewStream(nil)
	assert(ctx, outputStream != nil, "unable to create a new output stream")
	codecParams.Copy(outputStream.CodecParameters())
	outputStream.SetTimeBase(timeBase)
	outputStream.SetIndex(outputStreamIndex)
	logger.Debugf(
		ctx,
		"new output stream %d: %s: %s: %s: %s: %s",
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	return outputStream, nil
}

func (m *MapStreamIndices) sendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	if src := input.GetSource(); src != nil {
		// Capture the upstream source so GetDecoder can forward through
		// to the upstream decoder; sendInput runs under m.Locker, so this
		// store is safe against concurrent GetDecoder readers.
		m.upstreamSource = src
	}

	outputStreamIndexes, err := m.getOutputStreamIndexes(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to obtain the output stream indexes (on input: %#+v): %w", input, err)
	}

	for _, outputStreamIndex := range outputStreamIndexes {
		outputStream, err := m.getOutputStreamByIndex(
			ctx,
			outputStreamIndex,
			input.GetCodecParameters(),
			input.GetTimeBase(),
		)
		if err != nil {
			return fmt.Errorf("unable to get an output stream: %w", err)
		}

		out := input.CloneAsReferencedOutput()

		p, f := out.Unwrap()
		if p != nil {
			p.StreamInfo = packet.BuildStreamInfo(
				outputStream,
				m,
				p.PipelineSideData,
			)
		} else if f != nil {
			// Keep the upstream source on frames so downstream encoders can
			// type-assert to codec.GetDecoderer and bind to the upstream
			// decoder's resources (MediaCodec surface passthrough). Replacing
			// the source with `m` would force every per-stream encoder init
			// to consult MapStreamIndices.GetDecoder; that single-slot lookup
			// races with concurrent inputs from sibling streams (audio
			// frames overwrite the slot just-set by a video frame, etc.).
			frameSource := input.GetSource()
			if frameSource == nil {
				// frame.Source is fmt.Stringer; AbstractSource is also
				// fmt.Stringer — *MapStreamIndices satisfies both — fall
				// back to `m` only when the upstream did not advertise a
				// source at all (e.g. synthetic frames).
				frameSource = m
			}
			f.StreamInfo = frame.BuildStreamInfo(
				frameSource,
				f.GetCodecParameters(),
				outputStream.Index(),
				len(m.outputStreams),
				f.GetTimeBase(),
				f.GetDuration(),
				f.GetPipelineSideData(),
			)
		}
		out.SetStreamIndex(outputStream.Index())

		m.Locker.UDo(ctx, func() {
			select {
			case outputCh <- out:
			case <-ctx.Done():
			}
		})
	}
	return ctx.Err()
}

func (m *MapStreamIndices) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	m.Locker.Do(ctx, func() {
		callback(m.formatContext)
	})
}

func (m *MapStreamIndices) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	m.Locker.Do(ctx, func() {
		callback(m.formatContext)
	})
}

func (m *MapStreamIndices) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_ret error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _ret) }()

	var errs []error
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		m.Locker.Do(ctx, func() {
			for _, inputStream := range fmtCtx.Streams() {
				outputStreamIndexes, err := m.getOutputStreamIndexes(
					ctx,
					packetorframe.InputUnion{
						Packet: ptr(packet.BuildInput(
							nil,
							packet.BuildStreamInfo(
								inputStream,
								source,
								nil,
							),
						)),
					},
				)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to obtain the output stream indexes for input stream %d: %w", inputStream.Index(), err))
					continue
				}
				for _, outputStreamIndex := range outputStreamIndexes {
					outputStream, err := m.getOutputStreamByIndex(
						ctx,
						outputStreamIndex,
						inputStream.CodecParameters(),
						inputStream.TimeBase(),
					)
					if outputStream != nil {
						logger.Debugf(ctx, "made sure stream #%d (<-%d) is initialized", outputStream.Index(), inputStream.Index())
					} else {
						logger.Debugf(ctx, "no output stream for stream <-%d", inputStream.Index())
					}
					if err != nil {
						errs = append(errs, fmt.Errorf("unable to initialize an output stream #%d for input stream %d from source %s: %w", outputStreamIndex, inputStream.Index(), source, err))
					}
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (m *MapStreamIndices) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(m)
}

// GetDecoder forwards to the upstream source's decoder when the upstream
// source implements codec.GetDecoderer. This lets downstream encoder
// construction (which type-asserts frame.Source to codec.GetDecoderer in
// kernel/encoder.go:initEncoderFor) discover the upstream decoder even
// when MapStreamIndices sits between the decoder and the encoder.
func (m *MapStreamIndices) GetDecoder() *codec.Decoder {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &m.Locker, func() *codec.Decoder {
		if m.upstreamSource == nil {
			return nil
		}
		getDecoderer, ok := m.upstreamSource.(codec.GetDecoderer)
		if !ok {
			return nil
		}
		return getDecoderer.GetDecoder()
	})
}

func (m *MapStreamIndices) String() string {
	return "MapStreamIndices"
}

func (m *MapStreamIndices) Close(ctx context.Context) error {
	m.ClosureSignaler.Close(ctx)
	return nil
}

func (m *MapStreamIndices) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
