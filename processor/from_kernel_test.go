package processor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// mockKernel implements kernel.Abstract (types.Abstract) for testing.
// It satisfies: fmt.Stringer, types.Closer, types.GetObjectIDer, CloseChaner,
// SendInputer, and Generator.
type mockKernel struct {
	SendInputFn func(
		ctx context.Context,
		input packetorframe.InputUnion,
		outputCh chan<- packetorframe.OutputUnion,
	) error
	SendInputCallCount int

	CloseFn        func(context.Context) error
	CloseCallCount int

	CloseChanFn        func() <-chan struct{}
	CloseChanCallCount int

	GenerateFn        func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error
	GenerateCallCount int

	StringVal string
}

var _ types.Abstract = (*mockKernel)(nil)

func (m *mockKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	m.SendInputCallCount++
	if m.SendInputFn != nil {
		return m.SendInputFn(ctx, input, outputCh)
	}
	return nil
}

func (m *mockKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(m)
}

func (m *mockKernel) String() string {
	if m.StringVal != "" {
		return m.StringVal
	}
	return "mockKernel"
}

func (m *mockKernel) Close(ctx context.Context) error {
	m.CloseCallCount++
	if m.CloseFn != nil {
		return m.CloseFn(ctx)
	}
	return nil
}

func (m *mockKernel) CloseChan() <-chan struct{} {
	m.CloseChanCallCount++
	if m.CloseChanFn != nil {
		return m.CloseChanFn()
	}
	return nil
}

func (m *mockKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	m.GenerateCallCount++
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx, outputCh)
	}
	return nil
}

// --- NewFromKernel tests ---

func TestNewFromKernel_CreatesWithNonNilChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p, "NewFromKernel should return a non-nil processor")

	assert.NotNil(t, p.InputChan(), "InputChan should not be nil")
	assert.NotNil(t, p.OutputChan(), "OutputChan should not be nil")
	assert.NotNil(t, p.ErrorChan(), "ErrorChan should not be nil")
	assert.NotNil(t, p.CountersPtr(), "CountersPtr should not be nil")

	// Clean up
	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestNewFromKernel_CustomQueueSizes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(5),
		OptionQueueSizeError(3),
	)
	require.NotNil(t, p)

	// Verify channels were created with correct capacities.
	// The InputCh is a buffered channel with capacity 10.
	assert.Equal(t, 10, cap(p.ChanStruct.InputCh))
	assert.Equal(t, 5, cap(p.ChanStruct.OutputCh))
	assert.Equal(t, 3, cap(p.ChanStruct.ErrorCh))

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestNewFromKernel_KernelStored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{StringVal: "test-kernel"}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)

	assert.Equal(t, k, p.Kernel, "Kernel should be stored in the processor")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- Close tests ---

func TestFromKernel_Close_Success(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_Close_DoubleCloseSafety(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closeCalled := 0
	k := &mockKernel{
		CloseFn: func(ctx context.Context) error {
			closeCalled++
			return nil
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k)

	err1 := p.Close(ctx)
	assert.NoError(t, err1)

	err2 := p.Close(ctx)
	assert.NoError(t, err2)

	// The kernel's Close should only have been called once due to closeOnce.
	assert.Equal(t, 1, closeCalled, "kernel Close should only be called once due to sync.Once")
}

// --- CountersPtr tests ---

func TestFromKernel_CountersPtr_ReturnsConsistentPointer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	ptr1 := p.CountersPtr()
	ptr2 := p.CountersPtr()
	require.NotNil(t, ptr1)
	assert.Same(t, ptr1, ptr2, "CountersPtr should return the same pointer on multiple calls")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- String tests ---

func TestFromKernel_String_DelegatesToKernel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{StringVal: "my-special-kernel"}
	p := NewFromKernel[*mockKernel](ctx, k)

	assert.Equal(t, "my-special-kernel", p.String())

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_String_DefaultMockKernel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	assert.Equal(t, "mockKernel", p.String())

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- GetPacketSource tests ---

func TestFromKernel_GetPacketSource_KernelDoesNotImplement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	source := p.GetPacketSource()
	assert.Nil(t, source, "GetPacketSource should return nil when kernel does not implement packet.Source")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_GetPacketSink_KernelDoesNotImplement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	sink := p.GetPacketSink()
	assert.Nil(t, sink, "GetPacketSink should return nil when kernel does not implement packet.Sink")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- GetKernel tests ---

func TestFromKernel_GetKernel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{StringVal: "stored-kernel"}
	p := NewFromKernel[*mockKernel](ctx, k)

	got := p.GetKernel()
	assert.Equal(t, k, got)

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- Channel accessor tests ---

func TestFromKernel_InputChan_IsWritable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k,
		OptionQueueSizeInput(5),
	)

	ch := p.InputChan()
	require.NotNil(t, ch)
	// Verify the channel type is send-only and non-nil.
	// We do not send to it here because sending an empty InputUnion with
	// nil Packet and nil Frame would cause a panic in the readerLoop.

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_OutputChan_IsReadable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	ch := p.OutputChan()
	require.NotNil(t, ch)

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_ErrorChan_IsReadable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	ch := p.ErrorChan()
	require.NotNil(t, ch)

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- GetInternalQueueSize tests ---

func TestFromKernel_GetInternalQueueSize_NotImplemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	result := p.GetInternalQueueSize(ctx)
	assert.Nil(t, result, "should return nil when kernel doesn't implement GetInternalQueueSizer")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- HandleError tests ---

func TestFromKernel_HandleError_KernelNotErrorHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	testErr := assert.AnError
	result := p.HandleError(ctx, testErr)
	assert.Equal(t, testErr, result, "should return the same error when kernel doesn't implement ErrorHandler")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- IsDirty / Flush tests ---

func TestFromKernel_IsDirty_KernelNotFlusher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	assert.False(t, p.IsDirty(ctx), "should return false when kernel doesn't implement Flusher")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_Flush_KernelNotFlusher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	err := p.Flush(ctx)
	assert.NoError(t, err, "should return nil when kernel doesn't implement Flusher")

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- WithNetworkConn tests ---

func TestFromKernel_WithNetworkConn_NotImplemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	err := p.WithNetworkConn(ctx, nil)
	assert.Error(t, err)
	var notImpl ErrNotImplemented
	assert.ErrorAs(t, err, &notImpl)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- WithRawNetworkConn tests ---

func TestFromKernel_WithRawNetworkConn_NotImplemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	err := p.WithRawNetworkConn(ctx, nil)
	assert.Error(t, err)
	var notImpl ErrNotImplemented
	assert.ErrorAs(t, err, &notImpl)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- GetOldestDTSInTheQueue tests ---

func TestFromKernel_GetOldestDTSInTheQueue_NotImplemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)

	dur, err := p.GetOldestDTSInTheQueue(ctx)
	assert.Error(t, err)
	assert.Equal(t, time.Duration(0), dur)
	var notImpl ErrNotImplemented
	assert.ErrorAs(t, err, &notImpl)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}
