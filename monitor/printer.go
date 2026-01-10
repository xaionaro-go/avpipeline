package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	avpipeline_proto "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	goconvlibav "github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
)

const eventFormatString = "%-21s %-10s %-10s %-14s %-10s %-14s %-10s %-14s %-10s %-10s %-10s %-10s\n"

type PrintOptions struct {
	Format          string
	HighlightMissed time.Duration
}

func PrintMonitorEvents(ctx context.Context, out io.Writer, eventsCh <-chan *avpipeline_proto.MonitorEvent, options PrintOptions) error {
	format := options.Format
	switch format {
	case "plaintext":
		fmt.Fprintf(out, eventFormatString, "TS", "streamIdx", "PTS", "PTS", "DTS", "DTS", "dur", "dur", "size", "type", "frameFlags", "picType")
	case "json":
	default:
		return fmt.Errorf("unknown format: %q", format)
	}

	type streamState struct {
		lastEndPTS       int64
		lastEndTimestamp int64
	}
	streams := map[int]*streamState{}
	for ev := range eventsCh {
		state, ok := streams[int(ev.Stream.Index)]
		if !ok {
			fmt.Fprintf(out, "= new stream: %d; codec: 0x%X: time_base: %s\n", ev.Stream.Index, ev.Stream.CodecParameters.CodecId, ev.Stream.TimeBase)
			state = &streamState{}
			streams[int(ev.Stream.Index)] = state
		}
		switch format {
		case "plaintext":
			timeBase := goconvlibav.RationalFromProtobuf(ev.Stream.GetTimeBase())
			if ev.Packet != nil && len(ev.Frames) == 0 {
				pkt := ev.Packet
				if options.HighlightMissed > 0 && state.lastEndPTS != 0 && pkt.Pts > state.lastEndPTS {
					gapDur := AVConvDuration(pkt.Pts-state.lastEndPTS, timeBase)
					if gapDur >= options.HighlightMissed {
						fmt.Fprintf(out, "%-21d %-10d !!! MISSED PACKETS: gap from %d to %d (duration %s)\n",
							state.lastEndTimestamp, ev.Stream.Index, state.lastEndPTS, pkt.Pts, gapDur)
					}
				}
				printRow(out,
					ev.GetTimestampNs(),
					uint32(ev.Stream.Index),
					pkt.Pts,
					pkt.Dts,
					pkt.Duration,
					int64(pkt.DataSize),
					ev.Stream.CodecParameters.GetCodecType(),
					0,
					0,
					true,
					timeBase,
				)
				state.lastEndPTS = pkt.Pts + pkt.Duration
				state.lastEndTimestamp = int64(ev.GetTimestampNs()) + int64(AVConvDuration(pkt.Duration, timeBase))
			}
			for _, frame := range ev.Frames {
				if options.HighlightMissed > 0 && state.lastEndPTS != 0 && frame.Pts > state.lastEndPTS {
					gapDur := AVConvDuration(frame.Pts-state.lastEndPTS, timeBase)
					if gapDur >= options.HighlightMissed {
						fmt.Fprintf(out, "%-21d %-10d !!! MISSED FRAMES: gap from %d to %d (duration %s)\n",
							state.lastEndTimestamp, ev.Stream.Index, state.lastEndPTS, frame.Pts, gapDur)
					}
				}
				printRow(out,
					ev.GetTimestampNs(),
					uint32(ev.Stream.Index),
					frame.Pts,
					frame.PktDts,
					frame.Duration,
					int64(frame.DataSize),
					ev.Stream.CodecParameters.GetCodecType(),
					uint32(frame.Flags),
					uint32(frame.PictType),
					false,
					timeBase,
				)
				state.lastEndPTS = frame.Pts + frame.Duration
				state.lastEndTimestamp = int64(ev.GetTimestampNs()) + int64(AVConvDuration(frame.Duration, timeBase))
			}
		case "json":
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			err := enc.Encode(ev)
			if err != nil {
				return fmt.Errorf("unable to encode JSON: %w", err)
			}
		}
	}
	return nil
}

func printRow(
	out io.Writer,
	tsNs uint64,
	streamIdx uint32,
	pts, dts, duration int64,
	size int64,
	codecType int32,
	flags uint32,
	pictType uint32,
	isPacket bool,
	timeBase *goconvlibav.Rational,
) {
	flagsStr := "-"
	pictTypeStr := "-"
	if !isPacket {
		flagsStr = fmt.Sprintf("0x%08X", flags)
		pictTypeStr = fmt.Sprintf("0x%08X", pictType)
	}

	fmt.Fprintf(out, eventFormatString,
		fmt.Sprintf("%d", tsNs),
		fmt.Sprintf("%d", streamIdx),
		fmt.Sprintf("%d", pts),
		AVConvDuration(pts, timeBase),
		fmt.Sprintf("%d", dts),
		AVConvDuration(dts, timeBase),
		fmt.Sprintf("%d", duration),
		AVConvDuration(duration, timeBase),
		fmt.Sprintf("%d", size),
		fmt.Sprintf("%d", codecType),
		flagsStr,
		pictTypeStr,
	)
}

func AVConvDuration(pts int64, timeBase *goconvlibav.Rational) time.Duration {
	if timeBase == nil || timeBase.D == 0 {
		return 0
	}
	return time.Duration(int64(time.Second) * pts * timeBase.N / timeBase.D)
}

func ParseEventType(s string) (avpipeline_proto.MonitorEventType, error) {
	switch strings.ToLower(s) {
	case "send":
		return avpipeline_proto.MonitorEventType_EVENT_TYPE_SEND, nil
	case "receive":
		return avpipeline_proto.MonitorEventType_EVENT_TYPE_RECEIVE, nil
	case "kernel_output_send":
		return avpipeline_proto.MonitorEventType_EVENT_TYPE_KERNEL_OUTPUT_SEND, nil
	default:
		return avpipeline_proto.MonitorEventType_UNDEFINED_EVENT_TYPE, fmt.Errorf("unknown event type: %q", s)
	}
}
