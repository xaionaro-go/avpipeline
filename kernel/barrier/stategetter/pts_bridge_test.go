// pts_bridge_test.go contains tests for the per-chain PTS offset bridge
// applied at SwitchOutput.GetState boundaries.

package stategetter

import (
	"context"
	"sync"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type ptsBridgeMockSource struct {
	name string
}

func (m *ptsBridgeMockSource) String() string { return m.name }
func (m *ptsBridgeMockSource) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}

// makePTSPacket builds a packetorframe.InputUnion with a real *astiav.Packet so
// SetPTS / SetDTS mutations propagate through the same path as production code.
// mediaType drives StreamInfo.GetMediaType so the bridge can key per-stream the
// way it does cross-chain.
func makePTSPacket(
	t *testing.T,
	pts, dts int64,
	streamIdx int,
	mediaType astiav.MediaType,
	src packet.Source,
) packetorframe.InputUnion {
	t.Helper()
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	pkt.SetStreamIndex(streamIdx)

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(mediaType)

	si := &packet.StreamInfo{
		CodecParameters: cp,
		Source:          src,
		// 1/90000 = a representative video timebase. Using a single timebase across
		// the test keeps the duration arithmetic sane and matches the rtmp / camera
		// scenario described in /tmp/pts_continuity_inputswitch.md.
		TimeBase: astiav.NewRational(1, 90000),
	}
	p := packet.BuildInput(pkt, si)
	return packetorframe.InputUnion{Packet: &p}
}

// TestSwitchPTSBridge_NoFlag_NoMutation guards the off-by-default contract: when
// SwitchFlagBridgePTSAcrossChains is unset, the Switch must not touch PTS / DTS.
func TestSwitchPTSBridge_NoFlag_NoMutation(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)

	src := &ptsBridgeMockSource{name: "chainA"}
	pkt := makePTSPacket(t, 1000, 1000, 0, astiav.MediaTypeVideo, src)

	out0 := sw.Output(0)
	state, _ := out0.GetState(ctx, pkt)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 1000, pkt.GetPTS())
	assert.EqualValues(t, 1000, pkt.GetDTS())
}

// TestSwitchPTSBridge_NoSwitch_NoMutation: when the chain never changes the
// bridge must be a pass-through (no offset applied).
func TestSwitchPTSBridge_NoSwitch_NoMutation(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "chainA"}
	out0 := sw.Output(0)

	for _, pts := range []int64{1000, 2000, 3000} {
		pkt := makePTSPacket(t, pts, pts, 0, astiav.MediaTypeVideo, srcA)
		state, _ := out0.GetState(ctx, pkt)
		require.Equal(t, types.StatePass, state)
		assert.EqualValues(t, pts, pkt.GetPTS(), "no chain change => no offset")
		assert.EqualValues(t, pts, pkt.GetDTS())
	}
}

// TestSwitchPTSBridge_ForwardJump simulates camera (chain A, low PTS) → rtmp
// (chain B, ~600 s ahead). Without a bridge the player would see a forward leap
// of ~10 minutes; the bridge must rebase chain B's PTS so output is monotonic
// continuation of chain A by exactly 1 tick.
func TestSwitchPTSBridge_ForwardJump(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "chainA-camera"}
	srcB := &ptsBridgeMockSource{name: "chainB-rtmp"}

	outA := sw.Output(0)
	outB := sw.Output(1)

	// chain A emits up to PTS=100 (camera, low monotonic-epoch domain).
	pktA1 := makePTSPacket(t, 90, 90, 0, astiav.MediaTypeVideo, srcA)
	state, _ := outA.GetState(ctx, pktA1)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 90, pktA1.GetPTS())

	pktA2 := makePTSPacket(t, 100, 100, 0, astiav.MediaTypeVideo, srcA)
	state, _ = outA.GetState(ctx, pktA2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 100, pktA2.GetPTS())

	// switch to chain B (rtmp, ~600 s ahead at 1/90000 ⇒ 60000 ticks would be
	// only ~0.67 s; use 54_000_000_000 to mirror /tmp/pts_continuity_inputswitch.md).
	require.NoError(t, sw.SetValue(ctx, 1))

	// First chain B packet: raw PTS 54_000_000_000. Bridge must rebase to 101.
	pktB1 := makePTSPacket(t, 54_000_000_000, 54_000_000_000, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, pktB1)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 101, pktB1.GetPTS(), "chain B first pkt rebased to lastA+1")
	assert.EqualValues(t, 101, pktB1.GetDTS())

	// Second chain B packet: raw 54_000_000_010 (10 ticks later). Output 111.
	pktB2 := makePTSPacket(t, 54_000_000_010, 54_000_000_010, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, pktB2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 111, pktB2.GetPTS(), "chain B follow-up keeps offset")
	assert.EqualValues(t, 111, pktB2.GetDTS())
}

// TestSwitchPTSBridge_BackwardJump simulates rtmp (high PTS, chain A) → camera
// (low PTS, chain B). Camera's first PTS must be rebased forward to lastA+1.
func TestSwitchPTSBridge_BackwardJump(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "chainA-rtmp"}
	srcB := &ptsBridgeMockSource{name: "chainB-camera"}

	outA := sw.Output(0)
	outB := sw.Output(1)

	// chain A last emit PTS=60000.
	pktA := makePTSPacket(t, 60000, 60000, 0, astiav.MediaTypeVideo, srcA)
	state, _ := outA.GetState(ctx, pktA)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 60000, pktA.GetPTS())

	require.NoError(t, sw.SetValue(ctx, 1))

	// Camera first: raw PTS=50 (low). Bridge rebases to 60001.
	pktB1 := makePTSPacket(t, 50, 50, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, pktB1)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 60001, pktB1.GetPTS())
	assert.EqualValues(t, 60001, pktB1.GetDTS())

	// Camera follow-up raw=60 ⇒ 60011.
	pktB2 := makePTSPacket(t, 60, 60, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, pktB2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 60011, pktB2.GetPTS())
}

// TestSwitchPTSBridge_MultiStream verifies video and audio streams get
// independent offsets (their PTS sequences may diverge).
func TestSwitchPTSBridge_MultiStream(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "A"}
	srcB := &ptsBridgeMockSource{name: "B"}
	outA := sw.Output(0)
	outB := sw.Output(1)

	// Chain A: video last 200, audio last 500.
	pktAv := makePTSPacket(t, 200, 200, 0, astiav.MediaTypeVideo, srcA)
	pktAa := makePTSPacket(t, 500, 500, 1, astiav.MediaTypeAudio, srcA)
	for _, p := range []packetorframe.InputUnion{pktAv, pktAa} {
		state, _ := outA.GetState(ctx, p)
		require.Equal(t, types.StatePass, state)
	}

	require.NoError(t, sw.SetValue(ctx, 1))

	// Chain B video first raw=10 ⇒ 201.
	pktBv := makePTSPacket(t, 10, 10, 0, astiav.MediaTypeVideo, srcB)
	state, _ := outB.GetState(ctx, pktBv)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 201, pktBv.GetPTS(), "video offset bound separately")

	// Chain B audio first raw=30 ⇒ 501 (independent of video).
	pktBa := makePTSPacket(t, 30, 30, 1, astiav.MediaTypeAudio, srcB)
	state, _ = outB.GetState(ctx, pktBa)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 501, pktBa.GetPTS(), "audio offset independent of video")

	// Chain B follow-ups keep their respective offsets.
	pktBv2 := makePTSPacket(t, 20, 20, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, pktBv2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 211, pktBv2.GetPTS())

	pktBa2 := makePTSPacket(t, 40, 40, 1, astiav.MediaTypeAudio, srcB)
	state, _ = outB.GetState(ctx, pktBa2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 511, pktBa2.GetPTS())
}

// TestSwitchPTSBridge_ReturnToOldChain: A→B→A. On the second hop back to A the
// bridge must rebase A's "fresh" PTS off whatever B last emitted, not the stale
// pre-B value.
func TestSwitchPTSBridge_ReturnToOldChain(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "A"}
	srcB := &ptsBridgeMockSource{name: "B"}
	outA := sw.Output(0)
	outB := sw.Output(1)

	// A emits 100.
	p := makePTSPacket(t, 100, 100, 0, astiav.MediaTypeVideo, srcA)
	state, _ := outA.GetState(ctx, p)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 100, p.GetPTS())

	require.NoError(t, sw.SetValue(ctx, 1))
	// B raw=1000 ⇒ output 101.
	p = makePTSPacket(t, 1000, 1000, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, p)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 101, p.GetPTS())
	// B raw=1010 ⇒ output 111.
	p = makePTSPacket(t, 1010, 1010, 0, astiav.MediaTypeVideo, srcB)
	state, _ = outB.GetState(ctx, p)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 111, p.GetPTS())

	require.NoError(t, sw.SetValue(ctx, 0))
	// A raw=200 (its own clock advanced). Bridge must rebase to 112 (lastB+1),
	// not preserve 200 nor reuse the original A=0 offset.
	p = makePTSPacket(t, 200, 200, 0, astiav.MediaTypeVideo, srcA)
	state, _ = outA.GetState(ctx, p)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 112, p.GetPTS(), "return-to-A rebased off lastB+1")
}

// TestSwitchPTSBridge_NoPTSValue: a packet whose PTS is astiav.NoPtsValue must
// pass through untouched and must not pollute the bridge state.
func TestSwitchPTSBridge_NoPTSValue(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "A"}
	outA := sw.Output(0)

	p1 := makePTSPacket(t, 100, 100, 0, astiav.MediaTypeVideo, srcA)
	state, _ := outA.GetState(ctx, p1)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 100, p1.GetPTS())

	pNo := makePTSPacket(t, astiav.NoPtsValue, astiav.NoPtsValue, 0, astiav.MediaTypeVideo, srcA)
	state, _ = outA.GetState(ctx, pNo)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, astiav.NoPtsValue, pNo.GetPTS(),
		"NoPtsValue must remain NoPtsValue")

	// Subsequent valid packet must still see lastEmitted=100, not be perturbed.
	p2 := makePTSPacket(t, 110, 110, 0, astiav.MediaTypeVideo, srcA)
	state, _ = outA.GetState(ctx, p2)
	require.Equal(t, types.StatePass, state)
	assert.EqualValues(t, 110, p2.GetPTS())
}

// TestSwitchPTSBridge_RaceSafety hammers concurrent observation + switch to
// surface data races; relies on -race in CI.
func TestSwitchPTSBridge_RaceSafety(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagBridgePTSAcrossChains

	srcA := &ptsBridgeMockSource{name: "A"}
	srcB := &ptsBridgeMockSource{name: "B"}
	outA := sw.Output(0)
	outB := sw.Output(1)

	const N = 200
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			p := makePTSPacket(t, int64(100+i*10), int64(100+i*10), 0, astiav.MediaTypeVideo, srcA)
			outA.GetState(ctx, p)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			p := makePTSPacket(t, int64(50+i*10), int64(50+i*10), 0, astiav.MediaTypeVideo, srcB)
			outB.GetState(ctx, p)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			_ = sw.SetValue(ctx, int32(i&1))
		}
	}()
	wg.Wait()
}
