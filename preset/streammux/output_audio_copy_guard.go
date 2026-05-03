// output_audio_copy_guard.go provides the input-filter condition that keeps
// the audio-copy path strictly packet-only.

package streammux

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

// audioCopyAcceptsPacketsOnly returns a condition that passes through every
// non-audio item and every audio packet, while dropping audio frames with a
// rate-limited warning. It is wired in only when AudioCodec == NameCopy.
//
// Semantics:
//   - audio packet → pass (will be copy-encoded by EncoderCopy.sendPacket).
//   - non-audio    → pass (orthogonal stream; not our concern here).
//   - audio frame  → drop + warn (cannot be re-packetised without violating
//     "copy" semantics; the upstream should not have decoded this stream).
//
// Without this guard, an audio frame reaching the audio-copy path causes a
// fatal pipeline abort (BSF rejects frames; EncoderCopy.SendFrame returns
// codec.ErrCopyEncoder) which tears down every output, including unrelated
// video branches.
func (o *Output[C]) audioCopyAcceptsPacketsOnly() packetorframecondition.Condition {
	return packetorframecondition.Function(func(
		ctx context.Context,
		in packetorframe.InputUnion,
	) bool {
		if in.GetMediaType() != astiav.MediaTypeAudio {
			return true
		}
		if in.Packet != nil {
			return true
		}
		// Frame on a copy-coded audio stream: cannot pass through without
		// transcoding. Warn (rate-limiting handled by the logger backend) and
		// drop so the rest of the pipeline keeps running.
		logger.Warnf(
			ctx,
			"%s: dropping audio frame on copy-coded stream (upstream pre-decoded audio that '-c:a copy' cannot re-emit)",
			o,
		)
		return false
	})
}
