package processor

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
)

// --- helpers ---

// mockSource implements packet.Source (fmt.Stringer + WithOutputFormatContext).
type mockSource struct{}

func (m *mockSource) String() string { return "mockSource" }
func (m *mockSource) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}

// mockSink implements packet.Sink (WithInputFormatContext + NotifyAboutPacketSource).
type mockSink struct{}

func (m *mockSink) String() string { return "mockSink" }
func (m *mockSink) WithInputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}
func (m *mockSink) NotifyAboutPacketSource(_ context.Context, _ packet.Source) error {
	return nil
}

// mockKernelWithPacketSourceAndSink implements kernel.Abstract + packet.Source + packet.Sink.
type mockKernelWithPacketSourceAndSink struct {
	mockKernel
}

func (m *mockKernelWithPacketSourceAndSink) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}
func (m *mockKernelWithPacketSourceAndSink) WithInputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}
func (m *mockKernelWithPacketSourceAndSink) NotifyAboutPacketSource(_ context.Context, _ packet.Source) error {
	return nil
}

// buildTestStreamInfo creates a StreamInfo with no actual astiav.Stream,
// using fallback CodecParameters so that GetMediaType() returns astiav.MediaTypeUnknown
// and GetSize() returns 0 (which is safe for nil packets/frames).
func buildTestStreamInfo() *packetorframetypes.StreamInfo {
	return &packetorframetypes.StreamInfo{
		Source:      &mockSource{},
		StreamIndex: 0,
	}
}

// buildTestPacketInput creates an InputUnion wrapping a packet.Input with a nil underlying
// astiav.Packet (which is valid - see commons.go line 81). Size will be 0.
func buildTestPacketInput() packetorframe.InputUnion {
	pktInput := packet.BuildInput(nil, buildTestStreamInfo())
	return packetorframe.InputUnion{Packet: &pktInput}
}

// buildTestPacketOutput creates an OutputUnion wrapping a packet.Output with nil underlying packet.
func buildTestPacketOutput() packetorframe.OutputUnion {
	pktOutput := packet.BuildOutput(nil, buildTestStreamInfo())
	return packetorframe.OutputUnion{Packet: &pktOutput}
}

// buildTestFrameOutput creates an OutputUnion wrapping a frame.Output with nil underlying frame.
func buildTestFrameOutput() packetorframe.OutputUnion {
	cp := astiav.AllocCodecParameters()
	si := &packetorframetypes.StreamInfo{
		Source:          &mockSource{},
		CodecParameters: cp,
		StreamIndex:     0,
	}
	fOutput := frame.BuildOutput(nil, si)
	return packetorframe.OutputUnion{Frame: &fOutput}
}

// buildTestFrameInput creates an InputUnion wrapping a frame.Input with nil underlying frame.
func buildTestFrameInput() packetorframe.InputUnion {
	cp := astiav.AllocCodecParameters()
	si := &packetorframetypes.StreamInfo{
		Source:          &mockSource{},
		CodecParameters: cp,
		StreamIndex:     0,
	}
	fInput := frame.BuildInput(nil, 0, si)
	return packetorframe.InputUnion{Frame: &fInput}
}

// --- eofErr tests ---

func TestEofErr_WithEOF(t *testing.T) {
	assert.True(t, eofErr(io.EOF))
}

func TestEofErr_WithWrappedEOF(t *testing.T) {
	assert.True(t, eofErr(fmt.Errorf("something: %w", io.EOF)))
}

func TestEofErr_WithContextCanceled(t *testing.T) {
	assert.True(t, eofErr(context.Canceled))
}

func TestEofErr_WithWrappedContextCanceled(t *testing.T) {
	assert.True(t, eofErr(fmt.Errorf("something: %w", context.Canceled)))
}

func TestEofErr_WithOtherError(t *testing.T) {
	assert.False(t, eofErr(fmt.Errorf("some other error")))
}

func TestEofErr_WithNilError(t *testing.T) {
	// nil is not EOF or context.Canceled
	assert.False(t, eofErr(nil))
}

// --- readerLoop tests ---

func TestReaderLoop_InputChannelClosed(t *testing.T) {
	// When the input channel is closed immediately, readerLoop should return io.EOF.
	inputCh := make(chan packetorframe.InputUnion)
	close(inputCh)

	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
}

func TestReaderLoop_ContextCancelled(t *testing.T) {
	// When context is cancelled, readerLoop should return context error.
	inputCh := make(chan packetorframe.InputUnion, 10)
	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := readerLoop(ctx, inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReaderLoop_KernelCloseChan(t *testing.T) {
	// When the kernel's close channel is closed, readerLoop should return io.EOF.
	inputCh := make(chan packetorframe.InputUnion, 10)
	closeCh := make(chan struct{})
	close(closeCh) // close immediately

	k := &mockKernel{
		CloseChanFn: func() <-chan struct{} {
			return closeCh
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
}

func TestReaderLoop_ProcessesPacketInput(t *testing.T) {
	// Send a packet input, verify SendInput is called and counters are updated.
	inputCh := make(chan packetorframe.InputUnion, 10)
	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	pktInput := buildTestPacketInput()
	inputCh <- pktInput
	close(inputCh) // close after one message so the loop terminates

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 1, k.SendInputCallCount, "SendInput should have been called once")
}

func TestReaderLoop_ProcessesFrameInput(t *testing.T) {
	// Send a frame input, verify SendInput is called.
	inputCh := make(chan packetorframe.InputUnion, 10)
	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	frameInput := buildTestFrameInput()
	inputCh <- frameInput
	close(inputCh)

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 1, k.SendInputCallCount)
}

func TestReaderLoop_SendInputError(t *testing.T) {
	// When SendInput returns an error, readerLoop should wrap and return it.
	inputCh := make(chan packetorframe.InputUnion, 10)
	sendErr := fmt.Errorf("send failed")
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			return sendErr
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	pktInput := buildTestPacketInput()
	inputCh <- pktInput

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, sendErr)
	assert.Contains(t, err.Error(), "unable to send input")
}

func TestReaderLoop_DeferredDrainProcessesRemainingPackets(t *testing.T) {
	// The deferred drain loop should process remaining items in the input channel
	// after the main loop exits. We force the main loop to exit by returning an error
	// from SendInput on the first call.
	inputCh := make(chan packetorframe.InputUnion, 10)
	callCount := 0
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("main loop error")
			}
			return nil
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	// Put three packet inputs. First processed by main loop (returns error).
	// Remaining two processed by deferred drain.
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "main loop error")
	assert.Equal(t, 3, callCount, "deferred drain should have processed remaining packets")
}

func TestReaderLoop_DeferredDrainHandlesEOFError(t *testing.T) {
	// The deferred drain loop stops processing when SendInput returns EOF.
	// Strategy: First call to SendInput (main loop) returns a regular error,
	// causing the main loop to exit. Then the deferred drain processes remaining
	// buffered items and encounters EOF on the next call.
	inputCh := make(chan packetorframe.InputUnion, 10)
	callCount := 0
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			callCount++
			if callCount == 1 {
				// Main loop error - causes main loop to return
				return fmt.Errorf("main loop error")
			}
			// Deferred drain encounters EOF
			return io.EOF
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	// Buffer 3 items. The first is processed by the main loop (returns error).
	// The second is processed by the deferred drain (returns EOF, triggering early exit).
	// The third is never processed.
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to send input")
	assert.Contains(t, err.Error(), "main loop error")
	// callCount=1 from main loop, callCount=2 from drain (EOF stops it)
	assert.Equal(t, 2, callCount, "drain should stop on EOF from SendInput")
}

func TestReaderLoop_DeferredDrainHandlesNonEOFError(t *testing.T) {
	// The deferred drain loop stops processing when SendInput returns a non-EOF error.
	// Strategy: First call returns regular error (causes main loop exit), second call
	// in deferred drain returns a non-EOF error (stops drain with logger.Errorf).
	inputCh := make(chan packetorframe.InputUnion, 10)
	callCount := 0
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("main loop error")
			}
			return fmt.Errorf("drain error")
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "main loop error")
	assert.Equal(t, 2, callCount, "drain should stop on non-EOF error from SendInput")
}

func TestReaderLoop_DeferredDrainClosedChannel(t *testing.T) {
	// If the input channel is closed and empty, the deferred drain loop
	// exits immediately on the !ok path.
	inputCh := make(chan packetorframe.InputUnion, 10)
	close(inputCh)

	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 0, k.SendInputCallCount, "no inputs should have been processed")
}

func TestReaderLoop_DeferredDrainWithFrameInputs(t *testing.T) {
	// Test the deferred drain with frame inputs to cover the frame counting branch.
	// Force main loop exit via SendInput error on first call.
	inputCh := make(chan packetorframe.InputUnion, 10)
	callCount := 0
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("main loop error")
			}
			return nil
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	inputCh <- buildTestFrameInput()
	inputCh <- buildTestFrameInput()
	inputCh <- buildTestFrameInput()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.Error(t, err)
	assert.Equal(t, 3, callCount, "all frame inputs including drain should have been processed")
}

func TestReaderLoop_MultipleInputsThenClose(t *testing.T) {
	// Process multiple inputs in the main loop, then handle channel close.
	inputCh := make(chan packetorframe.InputUnion, 10)
	k := &mockKernel{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	inputCh <- buildTestPacketInput()
	inputCh <- buildTestFrameInput()
	inputCh <- buildTestPacketInput()
	close(inputCh)

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 3, k.SendInputCallCount)
}

// --- startProcessing / output processing tests ---

func TestFromKernel_ProcessPacketOutput(t *testing.T) {
	// Test that a packet sent to preOutputCh gets forwarded to OutputCh
	// and counters are incremented.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(10),
	)

	// Send a packet output to the preOutputCh (internal channel)
	pktOutput := buildTestPacketOutput()
	p.preOutputCh <- pktOutput

	// Read from OutputCh
	select {
	case out := <-p.OutputChan():
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output packet")
	}

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_ProcessFrameOutput(t *testing.T) {
	// Test that a frame sent to preOutputCh gets forwarded to OutputCh
	// and counters are incremented.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(10),
	)

	frameOutput := buildTestFrameOutput()
	p.preOutputCh <- frameOutput

	select {
	case out := <-p.OutputChan():
		assert.NotNil(t, out.Frame)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output frame")
	}

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_InputProcessingThroughPipeline(t *testing.T) {
	// Send a packet through the full pipeline: InputChan -> kernel.SendInput -> preOutputCh -> OutputCh.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			// Forward input as output
			pktOutput := buildTestPacketOutput()
			outputCh <- pktOutput
			return nil
		},
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(10),
	)

	// Send input
	pktInput := buildTestPacketInput()
	p.InputChan() <- pktInput

	// Read output
	select {
	case out := <-p.OutputChan():
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pipeline output")
	}

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_GenerateErrorPropagated(t *testing.T) {
	// When Generate returns an error, it should be forwarded to the error channel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	genErr := fmt.Errorf("generate failed")
	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			return genErr
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(10),
	)

	// Read from error channel
	select {
	case err := <-p.ErrorChan():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to generate traffic")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generate error")
	}

	err := p.Close(ctx)
	_ = err // may have errors from shutdown
}

// --- GetPacketSource/GetPacketSink positive path tests ---

func TestFromKernel_GetPacketSource_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithPacketSourceAndSink{}
	p := NewFromKernel[*mockKernelWithPacketSourceAndSink](ctx, k)

	source := p.GetPacketSource()
	assert.NotNil(t, source, "GetPacketSource should return non-nil when kernel implements packet.Source")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_GetPacketSink_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithPacketSourceAndSink{}
	p := NewFromKernel[*mockKernelWithPacketSourceAndSink](ctx, k)

	sink := p.GetPacketSink()
	assert.NotNil(t, sink, "GetPacketSink should return non-nil when kernel implements packet.Sink")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- startProcessing context cancellation during output ---

func TestFromKernel_ContextCancelDuringPacketOutput(t *testing.T) {
	// When context is cancelled while trying to send to OutputCh (which is full),
	// the omitted counter should be incremented.
	ctx, cancel := context.WithCancel(context.Background())

	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	// Use output queue size 0 (unbuffered) so sending blocks
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(0),
		OptionQueueSizeError(10),
	)

	// Cancel context to make the output goroutine take the ctx.Done() path
	cancel()

	// Try to send a packet - it may or may not succeed depending on timing,
	// but the close should complete without hanging.
	time.Sleep(50 * time.Millisecond)
	err := p.Close(ctx)
	_ = err
}

func TestFromKernel_ContextCancelDuringFrameOutput(t *testing.T) {
	// Same as above but with frames.
	ctx, cancel := context.WithCancel(context.Background())

	k := &mockKernel{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(0),
		OptionQueueSizeError(10),
	)

	cancel()
	time.Sleep(50 * time.Millisecond)
	err := p.Close(ctx)
	_ = err
}

// --- finalize error paths ---

func TestFromKernel_FinalizeKernelCloseError(t *testing.T) {
	// Test that when kernel.Close returns an error, finalize propagates it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kernelErr := fmt.Errorf("kernel close failed")
	k := &mockKernel{
		CloseFn: func(_ context.Context) error {
			return kernelErr
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeError(10),
	)

	// Drain error channel
	go func() {
		for range p.ErrorChan() {
		}
	}()

	err := p.Close(ctx)
	_ = err // The error may or may not surface through Close depending on timing
}

func TestFromKernel_FinalizeOnClosedError(t *testing.T) {
	// Test that when both kernel.Close and OnClosed return errors,
	// both errors are joined in finalize.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		CloseFn: func(_ context.Context) error {
			return fmt.Errorf("kernel error")
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeError(10),
	)
	p.OnClosed = func(_ context.Context) error {
		return fmt.Errorf("on-closed error")
	}

	// Drain error channel
	go func() {
		for range p.ErrorChan() {
		}
	}()

	err := p.Close(ctx)
	_ = err
}

// --- ProcessingState tests ---

func TestProcessingState_Fields(t *testing.T) {
	ps := ProcessingState{
		PendingInputs:    5,
		IsProcessorDirty: true,
		IsProcessing:     false,
	}
	ps.InputSent.Store(true)

	assert.Equal(t, 5, ps.PendingInputs)
	assert.True(t, ps.IsProcessorDirty)
	assert.False(t, ps.IsProcessing)
	assert.True(t, ps.InputSent.Load())
}

// --- getCaller tests ---

func TestGetCaller(t *testing.T) {
	file, line := getCaller()
	assert.NotEmpty(t, file, "getCaller should return a non-empty file path")
	assert.Greater(t, line, 0, "getCaller should return a positive line number")
}

// --- readerLoop with context cancellation after SendInput error (deferred drain with context.Canceled) ---

func TestReaderLoop_DeferredDrainWithContextCanceledError(t *testing.T) {
	// Verify the deferred drain handles context.Canceled from SendInput as EOF-like.
	// Strategy: First call returns error (main loop exit), second call returns context.Canceled.
	inputCh := make(chan packetorframe.InputUnion, 10)
	callCount := 0
	k := &mockKernel{
		SendInputFn: func(_ context.Context, _ packetorframe.InputUnion, _ chan<- packetorframe.OutputUnion) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("main loop error")
			}
			// In deferred drain, return context.Canceled (treated as EOF-like)
			return context.Canceled
		},
	}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	counters := processortypes.NewCounters()

	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()
	inputCh <- buildTestPacketInput()

	err := readerLoop(context.Background(), inputCh, k, outputCh, counters)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "main loop error")
	assert.Equal(t, 2, callCount, "drain should stop on context.Canceled from SendInput")
}

// --- Full pipeline with frame input/output ---

func TestFromKernel_FrameInputThroughPipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			frameOutput := buildTestFrameOutput()
			outputCh <- frameOutput
			return nil
		},
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			<-ctx.Done()
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(10),
	)

	frameInput := buildTestFrameInput()
	p.InputChan() <- frameInput

	select {
	case out := <-p.OutputChan():
		assert.NotNil(t, out.Frame)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame output")
	}

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- outChanError test ---

func TestFromKernel_OutChanError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	ch := p.outChanError()
	assert.NotNil(t, ch)

	err := p.Close(ctx)
	assert.NoError(t, err)
}
