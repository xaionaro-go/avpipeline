package kernel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/xaionaro-go/avpipeline/frame"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// fakeKernel implements the kernel.Abstract interface for tests.
type fakeKernel struct {
	mu      sync.Mutex
	packets []packet.Output
	frames  []frame.Output
	genErr  error
}

func (f *fakeKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	switch {
	case input.Packet != nil:
		f.mu.Lock()
		f.packets = append(f.packets, packet.BuildOutput(input.Packet.Packet, input.Packet.StreamInfo))
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputCh <- packetorframe.OutputUnion{Packet: ptr(packet.BuildOutput(input.Packet.Packet, input.Packet.StreamInfo))}:
		}
		return nil
	case input.Frame != nil:
		f.mu.Lock()
		f.frames = append(f.frames, frame.BuildOutput(input.Frame.Frame, input.Frame.StreamInfo))
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputCh <- packetorframe.OutputUnion{Frame: ptr(frame.BuildOutput(input.Frame.Frame, input.Frame.StreamInfo))}:
		}
		return nil
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
}

func (f *fakeKernel) Generate(ctx context.Context, _ chan<- packetorframe.OutputUnion) error {
	return f.genErr
}

func (f *fakeKernel) GetObjectID() globaltypes.ObjectID { return globaltypes.GetObjectID(f) }
func (f *fakeKernel) String() string                    { return "fake" }
func (f *fakeKernel) Close(context.Context) error       { return nil }
func (f *fakeKernel) CloseChan() <-chan struct{}        { return nil }

func TestTee_SendInput_Packet(t *testing.T) {
	ctx := context.Background()
	fk1 := &fakeKernel{}
	fk2 := &fakeKernel{}
	tks := Tee[Abstract]{fk1, fk2}

	outCh := make(chan packetorframe.OutputUnion, 4)

	in := packetorframe.InputUnion{Packet: &packet.Input{Packet: nil, StreamInfo: nil}}
	if err := tks.SendInput(ctx, in, outCh); err != nil {
		t.Fatalf("SendInput error: %v", err)
	}

	// collect outputs
	close(outCh)
	cnt := 0
	for range outCh {
		cnt++
	}
	if cnt != 2 {
		t.Fatalf("expected 2 packet outputs, got %d", cnt)
	}
}

func TestTee_SendInput_Frame(t *testing.T) {
	ctx := context.Background()
	fk1 := &fakeKernel{}
	fk2 := &fakeKernel{}
	tks := Tee[Abstract]{fk1, fk2}

	outCh := make(chan packetorframe.OutputUnion, 4)

	in := packetorframe.InputUnion{Frame: &frame.Input{Frame: nil, StreamInfo: nil}}
	if err := tks.SendInput(ctx, in, outCh); err != nil {
		t.Fatalf("SendInput error: %v", err)
	}

	close(outCh)
	cnt := 0
	for range outCh {
		cnt++
	}
	if cnt != 2 {
		t.Fatalf("expected 2 frame outputs, got %d", cnt)
	}
}

func TestTee_Generate(t *testing.T) {
	ctx := context.Background()
	fk1 := &fakeKernel{genErr: nil}
	fk2 := &fakeKernel{genErr: nil}
	tks := Tee[Abstract]{fk1, fk2}

	outCh := make(chan packetorframe.OutputUnion, 4)
	if err := tks.Generate(ctx, outCh); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
}

// closeFakeKernel allows customizing Close behavior and CloseChan for tests.
type closeFakeKernel struct {
	closeErr error
	closed   bool
	ch       chan struct{}
}

func (c *closeFakeKernel) SendInput(context.Context, packetorframe.InputUnion, chan<- packetorframe.OutputUnion) error {
	return nil
}
func (c *closeFakeKernel) Generate(context.Context, chan<- packetorframe.OutputUnion) error {
	return nil
}
func (c *closeFakeKernel) GetObjectID() globaltypes.ObjectID { return globaltypes.GetObjectID(c) }
func (c *closeFakeKernel) String() string                    { return "closeFake" }
func (c *closeFakeKernel) Close(ctx context.Context) error {
	c.closed = true
	return c.closeErr
}
func (c *closeFakeKernel) CloseChan() <-chan struct{} {
	return c.ch
}

func TestTee_ClosePropagationAndCloseChanMerge(t *testing.T) {
	ctx := context.Background()

	// kernel 1: closes without error and closes its channel after a short goroutine
	k1 := &closeFakeKernel{closeErr: nil, ch: make(chan struct{})}
	// kernel 2: returns an error on Close and has nil CloseChan
	k2 := &closeFakeKernel{closeErr: ErrCloseTest, ch: nil}

	// tee with both kernels
	tks := Tee[Abstract]{k1, k2}

	// start goroutine to close k1's channel after a moment
	go func() {
		// simulate some delay
		close(k1.ch)
	}()

	// call Close and expect aggregated error
	err := tks.Close(ctx)
	if err == nil {
		t.Fatalf("expected error from tee Close, got nil")
	}

	// ensure both kernels had Close called (k1.closed and k2.closed true)
	if !k1.closed || !k2.closed {
		t.Fatalf("expected Close called on both children: got k1=%v k2=%v", k1.closed, k2.closed)
	}

	// verify CloseChan merges non-nil child channel
	merged := tks.CloseChan()
	if merged == nil {
		t.Fatalf("expected merged close channel, got nil")
	}
	// wait for merged to close (it should be closed quickly by our goroutine)
	<-merged
}

// ErrCloseTest is an artificial error used in tests.
var ErrCloseTest = fmt.Errorf("close-test-error")

// blockingKernel is a test helper that blocks in Generate until its context is cancelled.
type blockingKernel struct {
	started chan struct{}
	stopped chan struct{}
}

func (b *blockingKernel) SendInput(context.Context, packetorframe.InputUnion, chan<- packetorframe.OutputUnion) error {
	return nil
}
func (b *blockingKernel) Generate(ctx context.Context, _ chan<- packetorframe.OutputUnion) error {
	close(b.started)
	<-ctx.Done()
	close(b.stopped)
	return ctx.Err()
}
func (b *blockingKernel) GetObjectID() globaltypes.ObjectID { return globaltypes.GetObjectID(b) }
func (b *blockingKernel) String() string                    { return "blocker" }
func (b *blockingKernel) Close(context.Context) error       { return nil }
func (b *blockingKernel) CloseChan() <-chan struct{}        { return nil }

func TestTee_GenerateCancellation(t *testing.T) {
	ctx := context.Background()

	// child1 will block until context is cancelled
	blockerStarted := make(chan struct{})
	blockerStopped := make(chan struct{})
	bk := &blockingKernel{started: blockerStarted, stopped: blockerStopped}

	// child2 returns an error immediately
	child2 := &fakeKernel{genErr: fmt.Errorf("gen-error")}

	tks := Tee[Abstract]{bk, child2}

	outCh := make(chan packetorframe.OutputUnion, 1)

	// run Generate (it should cancel blocker when child2 errors)
	err := tks.Generate(ctx, outCh)
	if err == nil {
		t.Fatalf("expected error from Generate, got nil")
	}
	// ensure blocker started then stopped (cancelled)
	<-blockerStarted
	<-blockerStopped
}

func TestTee_String(t *testing.T) {
	fk1 := &fakeKernel{}
	fk2 := &fakeKernel{}
	tks := Tee[Abstract]{fk1, fk2}
	s := tks.String()
	if !strings.Contains(s, "fake") || !strings.HasPrefix(s, "Tee(") {
		t.Fatalf("unexpected String output: %s", s)
	}
}
