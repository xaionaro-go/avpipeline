package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

type MapStreamIndices struct {
	*closuresignaler.ClosureSignaler
	Locker          xsync.Mutex
	PacketStreamMap map[InternalStreamKey][]int
	FrameStreamMap  map[int][]int
	Assigner        StreamIndexAssigner

	formatContext *astiav.FormatContext
	outputStreams map[int]*astiav.Stream
}

var _ Abstract = (*MapStreamIndices)(nil)
var _ packet.Source = (*MapStreamIndices)(nil)
var _ packet.Sink = (*MapStreamIndices)(nil)

type StreamIndexAssigner interface {
	StreamIndexAssign(context.Context, packetorframe.InputUnion) ([]int, error)
}

func NewMapStreamIndices(
	ctx context.Context,
	assigner StreamIndexAssigner,
) *MapStreamIndices {
	m := &MapStreamIndices{
		ClosureSignaler: closuresignaler.New(),
		PacketStreamMap: make(map[InternalStreamKey][]int),
		FrameStreamMap:  make(map[int][]int),
		Assigner:        assigner,

		formatContext: astiav.AllocFormatContext(),
		outputStreams: make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, m.formatContext)
	return m
}

func (m *MapStreamIndices) getOutputPacketStreamIndex(
	ctx context.Context,
	inputStreamIndex int,
	input packetorframe.InputUnion,
) (_ret []int, _err error) {
	streamKey := InternalStreamKey{
		StreamIndex: inputStreamIndex,
	}
	if input.Packet != nil {
		streamKey.Source = input.Packet.Source
	}
	if v, ok := m.PacketStreamMap[streamKey]; ok {
		return v, nil
	}
	defer func() {
		if _err != nil {
			return
		}
		logger.Debugf(ctx, "assigning indexes for %#+v: %v", streamKey, _ret)
		m.PacketStreamMap[streamKey] = _ret
	}()

	if m.Assigner == nil {
		return []int{input.GetStreamIndex()}, nil
	}

	return m.Assigner.StreamIndexAssign(ctx, input)
}

func (m *MapStreamIndices) getOutputFrameStreamIndex(
	ctx context.Context,
	input packetorframe.InputUnion,
) (_ret []int, _err error) {
	inputStreamIndex := input.Frame.StreamIndex
	if v, ok := m.FrameStreamMap[inputStreamIndex]; ok {
		return v, nil
	}
	defer func() {
		if _err != nil {
			return
		}
		logger.Debugf(ctx, "assigning index for %d: %v", inputStreamIndex, _ret)
		m.FrameStreamMap[inputStreamIndex] = _ret
	}()

	if m.Assigner == nil {
		return []int{inputStreamIndex}, nil
	}

	return m.Assigner.StreamIndexAssign(ctx, input)
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

func (m *MapStreamIndices) getOutputStreamForPacketByIndex(
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

func (m *MapStreamIndices) getOutputStreamForFrameByIndex(
	ctx context.Context,
	outputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := m.outputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	outputStream, err := m.newOutputStream(
		ctx,
		outputStreamIndex,
		codecParams, timeBase,
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
	outputStreamIndexes, err := m.getOutputPacketStreamIndex(
		ctx,
		input.Stream.Index(),
		packetorframe.InputUnion{Packet: &input},
	)
	if err != nil {
		return fmt.Errorf("unable to obtain the output stream indexes (on packet: %#+v): %w", input, err)
	}
	for _, outputStreamIndex := range outputStreamIndexes {
		outputStream, err := m.getOutputStreamForPacketByIndex(
			ctx,
			outputStreamIndex,
			input.Stream.CodecParameters(),
			input.Stream.TimeBase(),
		)
		if err != nil {
			return fmt.Errorf("unable to get an output stream: %w", err)
		}

		pkt := packet.CloneAsReferenced(input.Packet)
		pkt.SetStreamIndex(outputStream.Index())
		outPkt := packet.BuildOutput(
			pkt,
			packet.BuildStreamInfo(
				outputStream,
				m,
				input.PipelineSideData,
			),
		)
		m.Locker.UDo(ctx, func() {
			select {
			case outputPacketsCh <- outPkt:
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
				outputStreamIndexes, err := m.getOutputPacketStreamIndex(
					ctx,
					inputStream.Index(),
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
					outputStream, err := m.getOutputStreamForPacketByIndex(
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
	outputStreamIndexes, err := m.getOutputFrameStreamIndex(
		ctx,
		packetorframe.InputUnion{Frame: &input},
	)
	if err != nil {
		return fmt.Errorf("unable to obtain the output stream index (on frame: %#+v): %w", input, err)
	}
	for _, outputStreamIndex := range outputStreamIndexes {
		outputStream, err := m.getOutputStreamForFrameByIndex(
			ctx,
			outputStreamIndex,
			input.CodecParameters,
			input.TimeBase,
		)
		if err != nil {
			return fmt.Errorf("unable to get an output stream: %w", err)
		}

		outFrame := frame.BuildOutput(
			frame.CloneAsReferenced(input.Frame),
			input.Pos,
			frame.BuildStreamInfo(
				input.Source,
				input.CodecParameters,
				outputStream.Index(),
				len(m.outputStreams),
				input.StreamDuration,
				input.AvgFrameRate,
				input.TimeBase,
				input.StreamInfo.Duration,
				input.PipelineSideData,
			),
		)
		m.Locker.UDo(ctx, func() {
			select {
			case outputFramesCh <- outFrame:
			case <-ctx.Done():
			}
		})

	}
	return ctx.Err()
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
	m.ClosureSignaler.Close(ctx)
	return nil
}

func (m *MapStreamIndices) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return nil
}
