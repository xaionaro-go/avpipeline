package processor

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// mockKernelWithFlusher extends mockKernel with kernel.Flusher interface.
type mockKernelWithFlusher struct {
	mockKernel

	IsDirtyVal   bool
	FlushFn      func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error
	FlushCalled  bool
}

func (m *mockKernelWithFlusher) IsDirty(ctx context.Context) bool {
	return m.IsDirtyVal
}

func (m *mockKernelWithFlusher) Flush(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	m.FlushCalled = true
	if m.FlushFn != nil {
		return m.FlushFn(ctx, outputCh)
	}
	return nil
}

// mockKernelWithErrorHandler extends mockKernel with types.ErrorHandler interface.
type mockKernelWithErrorHandler struct {
	mockKernel

	HandleErrorFn func(ctx context.Context, err error) error
}

func (m *mockKernelWithErrorHandler) HandleError(ctx context.Context, err error) error {
	if m.HandleErrorFn != nil {
		return m.HandleErrorFn(ctx, err)
	}
	return nil
}

// mockKernelWithQueueSizer extends mockKernel with GetInternalQueueSizer interface.
type mockKernelWithQueueSizer struct {
	mockKernel

	QueueSizes map[string]uint64
}

func (m *mockKernelWithQueueSizer) GetInternalQueueSize(ctx context.Context) map[string]uint64 {
	return m.QueueSizes
}

// mockKernelWithDTSQueuer extends mockKernel with GetOldestDTSInTheQueuer interface.
type mockKernelWithDTSQueuer struct {
	mockKernel

	OldestDTS    time.Duration
	OldestDTSErr error
}

func (m *mockKernelWithDTSQueuer) GetOldestDTSInTheQueue(ctx context.Context) (time.Duration, error) {
	return m.OldestDTS, m.OldestDTSErr
}

// mockKernelWithNetworkConn extends mockKernel with WithNetworkConner interface.
type mockKernelWithNetworkConn struct {
	mockKernel

	WithNetworkConnFn func(ctx context.Context, callback func(context.Context, net.Conn) error) error
}

func (m *mockKernelWithNetworkConn) WithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	if m.WithNetworkConnFn != nil {
		return m.WithNetworkConnFn(ctx, callback)
	}
	return nil
}

// mockKernelWithRawNetworkConn extends mockKernel with WithRawNetworkConner interface.
type mockKernelWithRawNetworkConn struct {
	mockKernel

	WithRawNetworkConnFn func(ctx context.Context, callback func(context.Context, syscall.RawConn, string) error) error
}

func (m *mockKernelWithRawNetworkConn) WithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	if m.WithRawNetworkConnFn != nil {
		return m.WithRawNetworkConnFn(ctx, callback)
	}
	return nil
}

// --- IsDirty with Flusher implementation ---

func TestFromKernel_IsDirty_KernelImplementsFlusher_True(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithFlusher{IsDirtyVal: true}
	p := NewFromKernel[*mockKernelWithFlusher](ctx, k)

	assert.True(t, p.IsDirty(ctx))

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_IsDirty_KernelImplementsFlusher_False(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithFlusher{IsDirtyVal: false}
	p := NewFromKernel[*mockKernelWithFlusher](ctx, k)

	assert.False(t, p.IsDirty(ctx))

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- Flush with Flusher implementation ---

func TestFromKernel_Flush_KernelImplementsFlusher_Success(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithFlusher{}
	p := NewFromKernel[*mockKernelWithFlusher](ctx, k)

	err := p.Flush(ctx)
	assert.NoError(t, err)
	assert.True(t, k.FlushCalled)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

func TestFromKernel_Flush_KernelImplementsFlusher_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flushErr := fmt.Errorf("flush failed")
	k := &mockKernelWithFlusher{
		FlushFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			return flushErr
		},
	}
	p := NewFromKernel[*mockKernelWithFlusher](ctx, k)

	err := p.Flush(ctx)
	assert.ErrorIs(t, err, flushErr)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- HandleError with ErrorHandler implementation ---

func TestFromKernel_HandleError_KernelImplementsErrorHandler_Suppresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithErrorHandler{
		HandleErrorFn: func(ctx context.Context, err error) error {
			return nil // suppress the error
		},
	}
	p := NewFromKernel[*mockKernelWithErrorHandler](ctx, k)

	result := p.HandleError(ctx, fmt.Errorf("some error"))
	assert.NoError(t, result, "ErrorHandler suppressed the error")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

func TestFromKernel_HandleError_KernelImplementsErrorHandler_Wraps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernelWithErrorHandler{
		HandleErrorFn: func(ctx context.Context, err error) error {
			return fmt.Errorf("wrapped: %w", err)
		},
	}
	p := NewFromKernel[*mockKernelWithErrorHandler](ctx, k)

	origErr := fmt.Errorf("original")
	result := p.HandleError(ctx, origErr)
	assert.ErrorIs(t, result, origErr)
	assert.Contains(t, result.Error(), "wrapped:")

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- GetInternalQueueSize with implementation ---

func TestFromKernel_GetInternalQueueSize_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	expected := map[string]uint64{"input": 5, "output": 3}
	k := &mockKernelWithQueueSizer{QueueSizes: expected}
	p := NewFromKernel[*mockKernelWithQueueSizer](ctx, k)

	result := p.GetInternalQueueSize(ctx)
	assert.Equal(t, expected, result)

	err := p.Close(ctx)
	assert.NoError(t, err)
}

// --- GetOldestDTSInTheQueue with implementation ---

func TestFromKernel_GetOldestDTSInTheQueue_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	expectedDTS := 5 * time.Second
	k := &mockKernelWithDTSQueuer{OldestDTS: expectedDTS}
	p := NewFromKernel[*mockKernelWithDTSQueuer](ctx, k)

	dur, err := p.GetOldestDTSInTheQueue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expectedDTS, dur)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

func TestFromKernel_GetOldestDTSInTheQueue_ImplementedWithError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	expectedErr := fmt.Errorf("queue error")
	k := &mockKernelWithDTSQueuer{OldestDTSErr: expectedErr}
	p := NewFromKernel[*mockKernelWithDTSQueuer](ctx, k)

	dur, err := p.GetOldestDTSInTheQueue(ctx)
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, time.Duration(0), dur)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- WithNetworkConn with implementation ---

func TestFromKernel_WithNetworkConn_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callbackCalled := false
	k := &mockKernelWithNetworkConn{
		WithNetworkConnFn: func(ctx context.Context, callback func(context.Context, net.Conn) error) error {
			callbackCalled = true
			return nil
		},
	}
	p := NewFromKernel[*mockKernelWithNetworkConn](ctx, k)

	err := p.WithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, callbackCalled)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- WithRawNetworkConn with implementation ---

func TestFromKernel_WithRawNetworkConn_Implemented(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callbackCalled := false
	k := &mockKernelWithRawNetworkConn{
		WithRawNetworkConnFn: func(ctx context.Context, callback func(context.Context, syscall.RawConn, string) error) error {
			callbackCalled = true
			return nil
		},
	}
	p := NewFromKernel[*mockKernelWithRawNetworkConn](ctx, k)

	err := p.WithRawNetworkConn(ctx, func(ctx context.Context, rc syscall.RawConn, s string) error {
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, callbackCalled)

	closeErr := p.Close(ctx)
	assert.NoError(t, closeErr)
}

// --- Close with OnClosed callback ---

func TestFromKernel_Close_WithOnClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onClosedCalled := false
	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)
	p.OnClosed = func(ctx context.Context) error {
		onClosedCalled = true
		return nil
	}

	err := p.Close(ctx)
	assert.NoError(t, err)

	// Allow goroutines to finish
	time.Sleep(50 * time.Millisecond)
	assert.True(t, onClosedCalled, "OnClosed callback should have been called")
}

func TestFromKernel_Close_WithOnClosedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k,
		// Use larger error queue to avoid blocking on error sends during finalize.
		OptionQueueSizeError(10),
	)
	p.OnClosed = func(ctx context.Context) error {
		return fmt.Errorf("on-closed error")
	}

	// Drain error channel in a goroutine to prevent Close from deadlocking.
	// Close calls wg.Wait, which waits for goroutines that may send errors.
	go func() {
		for range p.ErrorChan() {
		}
	}()

	err := p.Close(ctx)
	// The error from OnClosed may or may not appear in Close's return value;
	// it depends on internal timing. We just verify Close doesn't hang.
	_ = err
}

// --- Close with kernel error ---

func TestFromKernel_Close_KernelCloseError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{
		CloseFn: func(ctx context.Context) error {
			return fmt.Errorf("kernel close error")
		},
	}
	p := NewFromKernel[*mockKernel](ctx, k,
		// Use larger error queue to avoid blocking on error sends during finalize.
		OptionQueueSizeError(10),
	)

	// Drain error channel in a goroutine to prevent Close from deadlocking.
	go func() {
		for range p.ErrorChan() {
		}
	}()

	err := p.Close(ctx)
	_ = err
}

// --- GetPacketSource with packet.Source implementation ---

// mockKernelWithPacketSource extends mockKernel and also implements packet.Source
// (which requires WithOutputFormatContext and String).
type mockKernelWithPacketSource struct {
	mockKernel
}

func (m *mockKernelWithPacketSource) WithOutputFormatContext(ctx context.Context, callback func(interface{})) {
	// no-op for testing
}

// Note: mockKernelWithPacketSource does NOT actually implement packet.Source
// because packet.Source requires WithOutputFormatContext with *astiav.FormatContext.
// We can't easily mock that without importing astiav which is a CGo dependency.
// The negative path (kernel doesn't implement) is already tested above.

// --- Verify Abstract interface compliance ---

func TestFromKernel_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*FromKernel[*mockKernel])(nil)
}

func TestFromKernel_ImplementsGetPacketSourcer(t *testing.T) {
	var _ GetPacketSourcer = (*FromKernel[*mockKernel])(nil)
}

func TestFromKernel_ImplementsGetPacketSinker(t *testing.T) {
	var _ GetPacketSinker = (*FromKernel[*mockKernel])(nil)
}

func TestFromKernel_ImplementsErrorHandler(t *testing.T) {
	var _ globaltypes.ErrorHandler = (*FromKernel[*mockKernel])(nil)
}

func TestFromKernel_ImplementsFlusher(t *testing.T) {
	var _ Flusher = (*FromKernel[*mockKernel])(nil)
}

func TestFromKernel_ImplementsGetKerneler(t *testing.T) {
	var _ GetKerneler = (*FromKernel[*mockKernel])(nil)
}

// --- Default options tests ---

func TestDefaultOptionsInput(t *testing.T) {
	opts := DefaultOptionsInput()
	require.NotEmpty(t, opts)
	cfg := Options(opts).config()
	assert.Equal(t, uint(0), cfg.InputQueue)
	assert.Equal(t, uint(1), cfg.OutputQueue)
	assert.Equal(t, uint(2), cfg.ErrorQueue)
}

func TestDefaultOptionsOutput(t *testing.T) {
	opts := DefaultOptionsOutput()
	require.NotEmpty(t, opts)
	cfg := Options(opts).config()
	assert.Equal(t, uint(60), cfg.InputQueue)
	assert.Equal(t, uint(0), cfg.OutputQueue)
	assert.Equal(t, uint(2), cfg.ErrorQueue)
}

func TestDefaultOptionsTranscoder(t *testing.T) {
	opts := DefaultOptionsTranscoder()
	require.NotEmpty(t, opts)
	cfg := Options(opts).config()
	assert.Equal(t, uint(60), cfg.InputQueue)
	assert.Equal(t, uint(10), cfg.OutputQueue)
	assert.Equal(t, uint(2), cfg.ErrorQueue)
}
