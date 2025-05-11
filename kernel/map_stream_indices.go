package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

type MapStreamIndices struct {
	*closeChan
	Locker          xsync.Mutex
	PacketStreamMap map[InternalStreamKey]typing.Optional[int]
	FrameStreamMap  map[int]typing.Optional[int]
	Assigner        StreamIndexAssigner

	formatContext *astiav.FormatContext
	outputStreams map[int]*astiav.Stream
}

var _ Abstract = (*MapStreamIndices)(nil)
var _ packet.Source = (*MapStreamIndices)(nil)
var _ packet.Sink = (*MapStreamIndices)(nil)

type StreamIndexAssigner interface {
	StreamIndexAssign(context.Context, types.InputPacketOrFrameUnion) (typing.Optional[int], error)
}

func NewMapStreamIndices(
	ctx context.Context,
	assigner StreamIndexAssigner,
) *MapStreamIndices {
	m := &MapStreamIndices{
		closeChan:       newCloseChan(),
		PacketStreamMap: make(map[InternalStreamKey]typing.Optional[int]),
		FrameStreamMap:  make(map[int]typing.Optional[int]),
		Assigner:        assigner,

		formatContext: astiav.AllocFormatContext(),
		outputStreams: make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, m.formatContext)
	return m
}

func (m *MapStreamIndices) getOutputPacketStreamIndex(
	ctx context.Context,
	stream *astiav.Stream,
	input types.InputPacketOrFrameUnion,
) (_ret typing.Optional[int], _err error) {
	streamKey := InternalStreamKey{
		StreamIndex: stream.Index(),
	}
	if input.Packet != nil {
		streamKey.Source = input.Packet.Source
	}
	if v, ok := m.PacketStreamMap[streamKey]; ok {
		return v, nil
	}

	var vOpt typing.Optional[int]
	defer func() {
		if _err == nil {
			m.PacketStreamMap[streamKey] = vOpt
		}
	}()

	if m.Assigner == nil {
		vOpt.Set(stream.Index())
	} else {
		var err error
		vOpt, err = m.Assigner.StreamIndexAssign(ctx, input)
		if err != nil {
			return typing.Optional[int]{}, err
		}
		if !vOpt.IsSet() {
			return typing.Optional[int]{}, nil
		}
	}

	v := vOpt.Get()
	logger.Debugf(ctx, "assigning index for %#+v: %d", streamKey, v)
	return vOpt, nil
}

func (m *MapStreamIndices) getOutputFrameStreamIndex(
	ctx context.Context,
	inputStreamIndex int,
	input types.InputPacketOrFrameUnion,
) (_ret typing.Optional[int], _err error) {
	if v, ok := m.FrameStreamMap[inputStreamIndex]; ok {
		return v, nil
	}

	var vOpt typing.Optional[int]
	defer func() {
		if _err == nil {
			m.FrameStreamMap[inputStreamIndex] = vOpt
		}
	}()
	if m.Assigner == nil {
		vOpt.Set(inputStreamIndex)
	} else {
		var err error
		vOpt, err = m.Assigner.StreamIndexAssign(ctx, input)
		if err != nil {
			return typing.Optional[int]{}, err
		}
		if !vOpt.IsSet() {
			return typing.Optional[int]{}, nil
		}
	}

	v := vOpt.Get()
	logger.Debugf(ctx, "assigning index for %d: %d", inputStreamIndex, v)
	return vOpt, nil
}

func (m *MapStreamIndices) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) error {
	return xsync.DoA3R1(
		ctx,
		&m.Locker,
		m.sendInputPacket,
		ctx, input, outputPacketsCh,
	)
}

func (m *MapStreamIndices) getOutputStreamForPacket(
	ctx context.Context,
	inputStream *astiav.Stream,
	input types.InputPacketOrFrameUnion,
) (*astiav.Stream, error) {
	outputStreamIndexOpt, err := m.getOutputPacketStreamIndex(ctx, inputStream, input)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain the output stream index (on packet: %#+v): %w", input, err)
	}
	if !outputStreamIndexOpt.IsSet() {
		return nil, nil
	}
	outputStreamIndex := outputStreamIndexOpt.Get()
	outputStream := m.outputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	outputStream, err = m.newOutputStream(
		ctx,
		outputStreamIndex,
		inputStream.CodecParameters(), inputStream.TimeBase(),
	)
	m.outputStreams[outputStreamIndex] = outputStream
	return outputStream, err
}

func (m *MapStreamIndices) getOutputStreamForFrame(
	ctx context.Context,
	inputStreamIndex int,
	codecCtx *astiav.CodecContext,
	timeBase astiav.Rational,
	input types.InputPacketOrFrameUnion,
) (*astiav.Stream, error) {
	outputStreamIndexOpt, err := m.getOutputFrameStreamIndex(ctx, inputStreamIndex, input)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain the output stream index (on packet: %#+v): %w", input, err)
	}
	if !outputStreamIndexOpt.IsSet() {
		return nil, nil
	}
	outputStreamIndex := outputStreamIndexOpt.Get()
	outputStream := m.outputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	codecParams := astiav.AllocCodecParameters()
	defer codecParams.Free()
	codecCtx.ToCodecParameters(codecParams)
	outputStream, err = m.newOutputStream(
		ctx,
		outputStreamIndex,
		codecParams, timeBase,
	)
	m.outputStreams[outputStreamIndex] = outputStream
	return outputStream, err
}

func (m *MapStreamIndices) newOutputStream(
	ctx context.Context,
	outputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := m.formatContext.NewStream(nil)
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

func (m *MapStreamIndices) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
) error {
	outputStream, err := m.getOutputStreamForPacket(
		ctx,
		input.Stream,
		types.InputPacketOrFrameUnion{Packet: &input},
	)
	if err != nil {
		return fmt.Errorf("unable to get an output stream: %w", err)
	}
	if outputStream == nil {
		return nil
	}

	pkt := packet.CloneAsReferenced(input.Packet)
	pkt.SetStreamIndex(outputStream.Index())
	outPkt := packet.BuildOutput(
		pkt,
		outputStream,
		m,
	)
	m.Locker.UDo(ctx, func() {
		select {
		case outputPacketsCh <- outPkt:
		case <-ctx.Done():
		}
	})
	return nil
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
				outputStream, err := m.getOutputStreamForPacket(
					ctx,
					inputStream,
					types.InputPacketOrFrameUnion{
						Packet: &packet.Input{
							Stream: inputStream,
							Source: source,
						},
					},
				)
				if outputStream != nil {
					logger.Debugf(ctx, "made sure stream #%d (<-%d) is initialized", outputStream.Index(), inputStream.Index())
				} else {
					logger.Debugf(ctx, "no output stream for stream <-%d", inputStream.Index())
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream for input stream %d from source %s: %w", inputStream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (m *MapStreamIndices) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA3R1(
		ctx,
		&m.Locker,
		m.sendInputFrame,
		ctx, input, outputFramesCh,
	)
}

func (m *MapStreamIndices) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputFramesCh chan<- frame.Output,
) error {
	outputStream, err := m.getOutputStreamForFrame(
		ctx,
		input.StreamIndex,
		input.CodecContext,
		input.TimeBase,
		types.InputPacketOrFrameUnion{Frame: &input},
	)
	if err != nil {
		return fmt.Errorf("unable to get an output stream: %w", err)
	}
	if outputStream == nil {
		return nil
	}

	outputFramesCh <- frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.CodecContext,
		outputStream.Index(),
		len(m.outputStreams),
		input.StreamDuration,
		input.TimeBase,
		input.Pos,
		input.Duration,
	)
	return nil
}

func (m *MapStreamIndices) String() string {
	ctx := context.TODO()
	if !m.Locker.ManualTryRLock(ctx) {
		return "MapStreamIndices"
	}
	defer m.Locker.ManualRUnlock(ctx)
	b, _ := json.Marshal(m.PacketStreamMap)
	return fmt.Sprintf("MapStreamIndices(%s)", b)
}

func (m *MapStreamIndices) Close(ctx context.Context) error {
	m.closeChan.Close(ctx)
	return nil
}

func (m *MapStreamIndices) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return nil
}
