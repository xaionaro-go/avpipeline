// from_kernel_reset_test.go: after the upstream Retryable reopens its
// inner kernel on EOF, the FromKernel processor wrapping a downstream
// Barrier holds stale packets in its InputCh / preOutputCh / OutputCh
// that were observed against the prior connection. Without draining,
// those stale packets back-pressure the chain and the upstream pusher
// hits "queue is full (size: 1)". Reset drains InputCh and OutputCh
// non-blockingly.

package processor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// TestFromKernel_Reset_DrainsPendingQueues plants stale packets in the
// processor's queues — InputCh, preOutputCh, OutputCh — then calls
// Reset and asserts every queue ends empty.
//
// To plant safely without racing the readerLoop / preOutputCh-copier,
// the mock kernel's SendInput holds an indefinite block on the input
// it receives — this wedges the readerLoop after it has consumed the
// first scheduled packet, so subsequent direct writes to InputCh
// (and the never-touched preOutputCh / OutputCh) survive intact.
//
// Pre-fix: FromKernel does not implement Resetter → assertion at the
// type-assertion site fails. Post-fix: Reset is wired and drains every
// queue.
func TestFromKernel_Reset_DrainsPendingQueues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// readerLoop reads from InputCh then calls SendInput. We block
	// SendInput indefinitely so the readerLoop never returns to read
	// another item — leaving InputCh's buffer for our plant.
	sendBlock := make(chan struct{})
	t.Cleanup(func() {
		// release the blocked SendInput so Close can finish (readerLoop
		// returns from SendInput and the goroutine exits via ctx
		// cancellation in the tested-call's Cleanup path).
		select {
		case <-sendBlock:
		default:
			close(sendBlock)
		}
	})

	var sendInputCalled sync.WaitGroup
	sendInputCalled.Add(1)
	var sendInputCalledOnce sync.Once
	k := &mockKernel{
		SendInputFn: func(ctx context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			sendInputCalledOnce.Do(sendInputCalled.Done)
			select {
			case <-sendBlock:
			case <-ctx.Done():
			}
			return nil
		},
		GenerateFn: func(ctx context.Context, _ chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	// Contract: FromKernel must implement Resetter so chain-restart
	// paths can drain stale queues uniformly.
	resetter, ok := any(p).(types.Resetter)
	require.True(t, ok, "FromKernel must implement kerneltypes.Resetter")

	// Send one priming packet to wedge readerLoop inside SendInput.
	primePkt := packet.Pool.Get()
	t.Cleanup(func() { packet.Pool.Put(primePkt) })
	primeIn := packet.BuildInput(primePkt, &packet.StreamInfo{})
	p.InputCh <- packetorframe.InputUnion{Packet: &primeIn}
	require.NoError(t, waitWG(&sendInputCalled, time.Second),
		"readerLoop must have entered SendInput within 1s; otherwise the plant below races with consumption")

	// Plant stale state in InputCh (size 1 by default; readerLoop is
	// wedged in SendInput so this buffer is now safe to populate).
	planInPkt := packet.Pool.Get()
	t.Cleanup(func() { packet.Pool.Put(planInPkt) })
	inObj := packet.BuildInput(planInPkt, &packet.StreamInfo{})
	select {
	case p.InputCh <- packetorframe.InputUnion{Packet: &inObj}:
	default:
		t.Fatal("plant: InputCh must accept one packet (readerLoop is blocked, expected free slot)")
	}
	require.Equal(t, 1, len(p.InputCh), "plant: InputCh must hold one stale packet")

	// Plant stale state in OutputCh (size 1 by default; the
	// preOutputCh-consumer is idle since the wedged kernel never
	// generated output).
	planOutPkt := packet.Pool.Get()
	t.Cleanup(func() { packet.Pool.Put(planOutPkt) })
	outObj := packet.BuildOutput(planOutPkt, &packet.StreamInfo{})
	select {
	case p.OutputCh <- packetorframe.OutputUnion{Packet: &outObj}:
	default:
		t.Fatal("plant: OutputCh must accept one packet (idle pre-output copier, expected free slot)")
	}
	require.Equal(t, 1, len(p.OutputCh), "plant: OutputCh must hold one stale packet")

	// --- The fix: Reset must drain both queues non-blockingly ---
	require.NoError(t, resetter.Reset(ctx))

	assert.Equal(t, 0, len(p.InputCh),
		"FromKernel.Reset must drain InputCh so a stale packet from the "+
			"prior connection does not block upstream pushes")
	assert.Equal(t, 0, len(p.OutputCh),
		"FromKernel.Reset must drain OutputCh so a stale packet from the "+
			"prior connection does not back-pressure downstream")
}

// TestFromKernel_Reset_ForwardsToKernelResetter asserts Reset still
// forwards to the wrapped kernel when it implements Resetter — the
// kernel-level state (e.g. Decoder per-stream codec contexts) must
// continue to clear, in addition to the new queue-drain step.
func TestFromKernel_Reset_ForwardsToKernelResetter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rk := &resettableMockKernel{}
	p := NewFromKernel[*resettableMockKernel](ctx, rk)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	resetter, ok := any(p).(types.Resetter)
	require.True(t, ok)
	require.NoError(t, resetter.Reset(ctx))

	assert.Equal(t, 1, rk.ResetCallCount,
		"FromKernel.Reset must forward to the wrapped kernel's Reset")
}

// resettableMockKernel extends mockKernel with a Reset method (closing
// the kerneltypes.Resetter interface) so we can assert FromKernel
// forwards Reset to the wrapped kernel.
type resettableMockKernel struct {
	mockKernel
	ResetCallCount int
}

func (m *resettableMockKernel) Reset(ctx context.Context) error {
	m.ResetCallCount++
	return nil
}

// waitWG waits for wg with a timeout; returns nil on done, error on
// timeout. Avoids hanging tests when readerLoop never schedules.
func waitWG(wg *sync.WaitGroup, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}
