// map_stream_indices_race_test.go: an in-flight SendInput on a
// MapStreamIndices must not race with concurrent
// NotifyAboutPacketSource / WithOutputFormatContext / sibling
// SendInput calls. The risk window is opened by sendInput releasing
// m.Locker via UDo around the outputCh send (map_stream_indices.go
// SendInput → sendInput → m.Locker.UDo at the per-output emit), which
// lets a sibling goroutine acquire m.Locker mid-emit and mutate the
// outputStreams map / formatContext while the first call still holds
// references into them.

package kernel

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// stressSource implements packet.Source for the stress test's
// NotifyAboutPacketSource pump (Source needs Stringer +
// WithOutputFormatContext).
type stressSource struct{ name string }

func (s *stressSource) String() string { return s.name }
func (s *stressSource) WithOutputFormatContext(
	_ context.Context,
	_ func(*astiav.FormatContext),
) {
}

// roundRobinAssigner returns a different output stream index per
// (streamIndex, source) pair so newOutputStream is exercised across
// many stream slots — the actual hot path in production where the
// MapStreamIndices serves a multi-track input (camera+microphone).
type roundRobinAssigner struct {
	mu    sync.Mutex
	next  int
	cache map[stressKey]int
}

type stressKey struct {
	streamIdx int
	source    any
}

func newRoundRobinAssigner() *roundRobinAssigner {
	return &roundRobinAssigner{cache: make(map[stressKey]int)}
}

func (a *roundRobinAssigner) StreamIndexAssign(
	_ context.Context,
	in packetorframe.InputUnion,
) ([]int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k := stressKey{streamIdx: in.GetStreamIndex(), source: in.GetSource()}
	if v, ok := a.cache[k]; ok {
		return []int{v}, nil
	}
	v := a.next
	a.next++
	a.cache[k] = v
	return []int{v}, nil
}

// TestMapStreamIndices_ConcurrentSendInput_NoRace pumps many
// concurrent SendInput / NotifyAboutPacketSource /
// WithOutputFormatContext calls against a single MapStreamIndices.
// The drain reader is intentionally throttled so the UDo-released
// window in sendInput stays open long enough for sibling acquisitions
// to interleave.
//
// Pass criteria: -race must not flag a data race; the test must not
// panic on the assertions in newOutputStream
// (codecParams != nil / formatContext != nil / outputStream != nil).
func TestMapStreamIndices_ConcurrentSendInput_NoRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		streams           = 8
		sendsPerStream    = 200
		notifierIters     = 50
		fmtCtxQueryIters  = 200
		drainReaderDelay  = 50 * time.Microsecond
	)

	m := NewMapStreamIndices(ctx, newRoundRobinAssigner())
	defer m.Close(ctx)

	// One *Dummy per stream; each is a distinct packetorframe source
	// so the assigner mints a fresh output index for it. The Dummy
	// satisfies the Stringer + AbstractSource shape used by the
	// MapStreamIndices source-pass-through path.
	sources := make([]*Dummy, streams)
	for i := range sources {
		sources[i] = &Dummy{}
	}

	cps := make([]*astiav.CodecParameters, streams)
	for i := range cps {
		cp := astiav.AllocCodecParameters()
		t.Cleanup(cp.Free)
		cp.SetMediaType(astiav.MediaTypeAudio)
		cp.SetCodecID(astiav.CodecIDPcmS16Le)
		cp.SetSampleRate(48000)
		cp.SetChannelLayout(astiav.ChannelLayoutMono)
		cps[i] = cp
	}

	outputCh := make(chan packetorframe.OutputUnion, 1)

	// Slow reader: drains outputCh with a small per-item sleep so the
	// UDo-released-lock window in sendInput stays open and sibling
	// SendInput goroutines have a real chance to interleave.
	var drained atomic.Uint64
	drainCtx, drainCancel := context.WithCancel(ctx)
	defer drainCancel()
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for {
			select {
			case <-drainCtx.Done():
				return
			case <-outputCh:
				drained.Add(1)
				time.Sleep(drainReaderDelay)
			}
		}
	}()

	// Sender goroutines: each owns one stream slot and sends sendsPerStream frames.
	var senderWG sync.WaitGroup
	for i := 0; i < streams; i++ {
		i := i
		senderWG.Add(1)
		go func() {
			defer senderWG.Done()
			si := &packetorframetypes.StreamInfo{
				Source:          sources[i],
				CodecParameters: cps[i],
				StreamIndex:     i,
				StreamsCount:    streams,
				TimeBase:        astiav.NewRational(1, 48000),
			}
			for j := 0; j < sendsPerStream; j++ {
				f := astiav.AllocFrame()
				f.SetNbSamples(1024)
				f.SetSampleRate(48000)
				f.SetChannelLayout(astiav.ChannelLayoutMono)
				f.SetSampleFormat(astiav.SampleFormatS16)
				input := packetorframe.InputUnion{
					Frame: ptr(frame.BuildInput(f, 0, si)),
				}
				if err := m.SendInput(ctx, input, outputCh); err != nil {
					if ctx.Err() != nil {
						f.Free()
						return
					}
					t.Errorf("stream %d send %d: SendInput: %v", i, j, err)
					f.Free()
					return
				}
				f.Free()
			}
		}()
	}

	// Concurrent WithOutputFormatContext queries — these contend
	// the same m.Locker as sendInput.
	var queryWG sync.WaitGroup
	queryWG.Add(1)
	go func() {
		defer queryWG.Done()
		for j := 0; j < fmtCtxQueryIters; j++ {
			m.WithOutputFormatContext(ctx, func(_ *astiav.FormatContext) {})
			if ctx.Err() != nil {
				return
			}
		}
	}()

	// Concurrent NotifyAboutPacketSource calls — exercise the same
	// outputStreams map population path from a different entry point.
	queryWG.Add(1)
	go func() {
		defer queryWG.Done()
		for j := 0; j < notifierIters; j++ {
			_ = m.NotifyAboutPacketSource(ctx, &stressSource{name: "stressSource"})
			if ctx.Err() != nil {
				return
			}
		}
	}()

	senderWG.Wait()
	queryWG.Wait()

	drainCancel()
	drainWG.Wait()

	t.Logf("drain absorbed %d frames", drained.Load())
	require.Greater(t, int(drained.Load()), 0,
		"drain reader must have consumed at least one frame "+
			"(otherwise the test never reached the UDo-released window)")
}
