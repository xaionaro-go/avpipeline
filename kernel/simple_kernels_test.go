// simple_kernels_test.go tests the simpler kernel types that don't require FFmpeg I/O.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	barriertypes "github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- helpers ---

func makeFrameInput(t *testing.T) (packetorframe.InputUnion, func()) {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)
	f := astiav.AllocFrame()
	f.SetWidth(64)
	f.SetHeight(64)
	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
	}
	frameInput := frame.BuildInput(f, 0, streamInfo)
	cleanup := func() {
		cp.Free()
		f.Free()
	}
	return packetorframe.InputUnion{Frame: &frameInput}, cleanup
}

func makePacketInput(t *testing.T) (packetorframe.InputUnion, func()) {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)
	pkt := astiav.AllocPacket()
	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		Stream:          nil,
	}
	pktInput := packet.BuildInput(pkt, streamInfo)
	cleanup := func() {
		cp.Free()
		pkt.Free()
	}
	return packetorframe.InputUnion{Packet: &pktInput}, cleanup
}

// --- Error type tests ---

func TestErrNotImplemented_WithError(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := ErrNotImplemented{Err: inner}
	testifyassert.Contains(t, err.Error(), "not implemented")
	testifyassert.Contains(t, err.Error(), "inner error")
	testifyassert.Equal(t, inner, err.Unwrap())
}

func TestErrNotImplemented_WithoutError(t *testing.T) {
	err := ErrNotImplemented{}
	testifyassert.Equal(t, "not implemented", err.Error())
	testifyassert.Nil(t, err.Unwrap())
}

func TestErrUnableToSetSendBufferSize(t *testing.T) {
	inner := fmt.Errorf("syscall failed")
	err := ErrUnableToSetSendBufferSize{Size: 65536, Err: inner}
	testifyassert.Contains(t, err.Error(), "65536")
	testifyassert.Contains(t, err.Error(), "syscall failed")
	testifyassert.Equal(t, inner, err.Unwrap())
}

func TestErrRetry_WithNilErr(t *testing.T) {
	err := ErrRetry{}
	testifyassert.Contains(t, err.Error(), "please retry")
}

func TestErrRetry_WithErr(t *testing.T) {
	inner := fmt.Errorf("connection reset")
	err := ErrRetry{Err: inner}
	testifyassert.Contains(t, err.Error(), "connection reset")
	testifyassert.Contains(t, err.Error(), "please retry")
}

func TestErrKernelNotSet(t *testing.T) {
	err := ErrKernelNotSet{}
	testifyassert.Equal(t, "kernel is not set", err.Error())
}

// --- Passthrough kernel tests ---

func TestPassthrough_String(t *testing.T) {
	p := Passthrough{}
	testifyassert.Equal(t, "Passthrough", p.String())
}

func TestPassthrough_GetObjectID(t *testing.T) {
	p := &Passthrough{}
	id := p.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestPassthrough_Close(t *testing.T) {
	p := Passthrough{}
	err := p.Close(context.Background())
	testifyassert.NoError(t, err)
}

func TestPassthrough_CloseChan(t *testing.T) {
	p := Passthrough{}
	ch := p.CloseChan()
	testifyassert.Nil(t, ch)
}

func TestPassthrough_Generate(t *testing.T) {
	p := Passthrough{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := p.Generate(context.Background(), outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 0)
}

func TestPassthrough_SendInput_Frame(t *testing.T) {
	p := Passthrough{}
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := p.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestPassthrough_SendInput_Packet(t *testing.T) {
	p := Passthrough{}
	input, cleanup := makePacketInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := p.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestPassthrough_SendInput_NilInput(t *testing.T) {
	p := Passthrough{}
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := p.SendInput(context.Background(), packetorframe.InputUnion{}, outputCh)
	testifyassert.Error(t, err)
	var unexpectedErr kerneltypes.ErrUnexpectedInputType
	testifyassert.True(t, errors.As(err, &unexpectedErr))
}

func TestPassthrough_SendInput_ContextCancelled(t *testing.T) {
	p := Passthrough{}
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use unbuffered channel so send blocks
	outputCh := make(chan packetorframe.OutputUnion)
	err := p.SendInput(ctx, input, outputCh)
	testifyassert.Error(t, err)
	testifyassert.True(t, errors.Is(err, context.Canceled))
}

func TestPassthrough_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*Passthrough)(nil)
}

// --- Filter kernel tests ---

func TestFilter_NewFilter(t *testing.T) {
	f := NewFilter(nil)
	testifyassert.NotNil(t, f)
	testifyassert.Nil(t, f.Condition)
}

func TestFilter_String(t *testing.T) {
	f := NewFilter(nil)
	testifyassert.Contains(t, f.String(), "Filter")
}

func TestFilter_GetObjectID(t *testing.T) {
	f := NewFilter(nil)
	id := f.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestFilter_Close(t *testing.T) {
	f := NewFilter(nil)
	err := f.Close(context.Background())
	testifyassert.NoError(t, err)
}

func TestFilter_CloseChan(t *testing.T) {
	f := NewFilter(nil)
	ch := f.CloseChan()
	testifyassert.NotNil(t, ch) // uses closuresignaler
}

func TestFilter_Generate(t *testing.T) {
	f := NewFilter(nil)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := f.Generate(context.Background(), outputCh)
	testifyassert.NoError(t, err)
}

func TestFilter_SendInput_NilCondition_PassesThrough(t *testing.T) {
	f := NewFilter(nil)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := f.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestFilter_SendInput_MatchingCondition(t *testing.T) {
	// Condition that always matches — should pass through
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	f := NewFilter(cond)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := f.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestFilter_SendInput_NonMatchingCondition(t *testing.T) {
	// Condition that never matches — should drop
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return false
	})
	f := NewFilter(cond)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := f.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 0) // Dropped
}

func TestFilter_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*Filter)(nil)
}

// --- Wait kernel tests ---

func TestWait_NewWait(t *testing.T) {
	w := NewWait(nil, 10)
	testifyassert.NotNil(t, w)
	testifyassert.Equal(t, uint(10), w.MaxQueueSize)
}

func TestWait_String(t *testing.T) {
	w := NewWait(nil, 10)
	testifyassert.Contains(t, w.String(), "Wait")
}

func TestWait_GetObjectID(t *testing.T) {
	w := NewWait(nil, 10)
	id := w.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestWait_Close(t *testing.T) {
	w := NewWait(nil, 10)
	err := w.Close(context.Background())
	testifyassert.NoError(t, err)
}

func TestWait_Generate(t *testing.T) {
	w := NewWait(nil, 10)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.Generate(context.Background(), outputCh)
	testifyassert.NoError(t, err)
}

func TestWait_SendInput_NilCondition_PassesThrough(t *testing.T) {
	w := NewWait(nil, 10)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestWait_SendInput_ConditionMatch_Buffers(t *testing.T) {
	// Condition that always matches — should buffer
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	w := NewWait(cond, 10)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 0) // Buffered, not sent
	testifyassert.Len(t, w.Queue, 1)
}

func TestWait_SendInput_ConditionRelease(t *testing.T) {
	count := 0
	// Match first 2, then stop matching to release
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		count++
		return count <= 2
	})
	w := NewWait(cond, 10)

	input1, cleanup1 := makeFrameInput(t)
	defer cleanup1()
	input2, cleanup2 := makeFrameInput(t)
	defer cleanup2()
	input3, cleanup3 := makeFrameInput(t)
	defer cleanup3()

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// First two inputs should be buffered
	err := w.SendInput(context.Background(), input1, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 0)

	err = w.SendInput(context.Background(), input2, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 0)

	// Third input stops matching — releases buffered + passes through
	err = w.SendInput(context.Background(), input3, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Equal(t, 3, len(outputCh)) // 2 buffered + 1 current
	testifyassert.Len(t, w.Queue, 0)
}

func TestWait_SendInput_MaxQueueSize(t *testing.T) {
	// Always buffer with max queue size 2
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	w := NewWait(cond, 2)

	outputCh := make(chan packetorframe.OutputUnion, 20)

	for i := 0; i < 5; i++ {
		input, cleanup := makeFrameInput(t)
		defer cleanup()
		err := w.SendInput(context.Background(), input, outputCh)
		testifyassert.NoError(t, err)
	}

	// Queue should be capped at 2
	testifyassert.Equal(t, 2, len(w.Queue))
}

func TestWait_SendInput_NilInput(t *testing.T) {
	w := NewWait(nil, 10)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), packetorframe.InputUnion{}, outputCh)
	testifyassert.Error(t, err)
	var unexpectedErr kerneltypes.ErrUnexpectedInputType
	testifyassert.True(t, errors.As(err, &unexpectedErr))
}

func TestWait_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*Wait)(nil)
}

// --- FrameCondition kernel tests ---

func TestFrameCondition_NewFrameCondition(t *testing.T) {
	fc := NewFrameCondition(nil)
	testifyassert.NotNil(t, fc)
}

func TestFrameCondition_String(t *testing.T) {
	fc := NewFrameCondition(nil)
	testifyassert.Equal(t, "AudioNormalize", fc.String())
}

func TestFrameCondition_GetObjectID(t *testing.T) {
	fc := NewFrameCondition(nil)
	id := fc.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestFrameCondition_Close(t *testing.T) {
	fc := NewFrameCondition(nil)
	err := fc.Close(context.Background())
	testifyassert.NoError(t, err)
}

func TestFrameCondition_CloseChan(t *testing.T) {
	fc := NewFrameCondition(nil)
	ch := fc.CloseChan()
	testifyassert.Nil(t, ch)
}

func TestFrameCondition_Generate(t *testing.T) {
	fc := NewFrameCondition(nil)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := fc.Generate(context.Background(), outputCh)
	testifyassert.NoError(t, err)
}

func TestFrameCondition_SendInput_Packet_ReturnsError(t *testing.T) {
	fc := NewFrameCondition(nil)
	input, cleanup := makePacketInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := fc.SendInput(context.Background(), input, outputCh)
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "does not process packets")
}

func TestFrameCondition_SendInput_NilInput(t *testing.T) {
	fc := NewFrameCondition(nil)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := fc.SendInput(context.Background(), packetorframe.InputUnion{}, outputCh)
	testifyassert.Error(t, err)
	var unexpectedErr kerneltypes.ErrUnexpectedInputType
	testifyassert.True(t, errors.As(err, &unexpectedErr))
}

func TestFrameCondition_SendInput_NilCondition_PassesThrough(t *testing.T) {
	fc := NewFrameCondition(nil)
	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := fc.SendInput(context.Background(), input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestFrameCondition_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*FrameCondition)(nil)
}

// --- FrameInfo tests ---

func TestFrameInfo_GetPictureType_Nil(t *testing.T) {
	var fi *FrameInfo
	pt := fi.GetPictureType()
	testifyassert.Equal(t, astiav.PictureType(math.MaxUint32), pt)
}

func TestFrameInfo_GetPictureType_NonNil(t *testing.T) {
	fi := &FrameInfo{PictureType: astiav.PictureTypeI}
	pt := fi.GetPictureType()
	testifyassert.Equal(t, astiav.PictureTypeI, pt)
}

func TestFrameInfo_Bytes_Roundtrip(t *testing.T) {
	fi := &FrameInfo{
		PTS:         100,
		DTS:         90,
		TimeBase:    astiav.NewRational(1, 30),
		Duration:    3000,
		StreamIndex: 1,
		FrameFlags:  astiav.FrameFlags(astiav.FrameFlagKey),
		PictureType: astiav.PictureTypeI,
	}
	b := fi.Bytes()
	testifyassert.Len(t, b, 64)

	fi2 := FrameInfoFromBytes(b)
	require.NotNil(t, fi2)
	testifyassert.Equal(t, fi.PTS, fi2.PTS)
	testifyassert.Equal(t, fi.DTS, fi2.DTS)
	testifyassert.Equal(t, fi.TimeBase.Num(), fi2.TimeBase.Num())
	testifyassert.Equal(t, fi.TimeBase.Den(), fi2.TimeBase.Den())
	testifyassert.Equal(t, fi.Duration, fi2.Duration)
	testifyassert.Equal(t, fi.StreamIndex, fi2.StreamIndex)
	testifyassert.Equal(t, fi.FrameFlags, fi2.FrameFlags)
	testifyassert.Equal(t, fi.PictureType, fi2.PictureType)
}

func TestFrameInfoFromBytes_TooShort(t *testing.T) {
	fi := FrameInfoFromBytes([]byte{1, 2, 3})
	testifyassert.Nil(t, fi)
}

func TestFrameInfoFromPacketInput_Found(t *testing.T) {
	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeVideo)
	pkt := astiav.AllocPacket()
	defer pkt.Free()

	fi := &FrameInfo{PTS: 42}
	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters:  cp,
		PipelineSideData: globaltypes.PipelineSideData{fi},
	}
	pktInput := packet.BuildInput(pkt, streamInfo)
	result := FrameInfoFromPacketInput(pktInput)
	testifyassert.NotNil(t, result)
	testifyassert.Equal(t, int64(42), result.PTS)
}

func TestFrameInfoFromPacketInput_NotFound(t *testing.T) {
	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeVideo)
	pkt := astiav.AllocPacket()
	defer pkt.Free()

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: cp,
	}
	pktInput := packet.BuildInput(pkt, streamInfo)
	result := FrameInfoFromPacketInput(pktInput)
	testifyassert.Nil(t, result)
}

// --- Barrier kernel tests ---

func TestBarrier_NewBarrier(t *testing.T) {
	ctx := context.Background()
	sw := barrierstategetter.NewSwitch()
	b := NewBarrier(ctx, sw.Output(0))
	testifyassert.NotNil(t, b)
}

func TestBarrier_String(t *testing.T) {
	ctx := context.Background()
	sw := barrierstategetter.NewSwitch()
	b := NewBarrier(ctx, sw.Output(0))
	s := b.String()
	testifyassert.Contains(t, s, "Barrier")
}

func TestBarrier_Pass(t *testing.T) {
	ctx := context.Background()
	sw := barrierstategetter.NewSwitch()
	sw.CurrentValue.Store(0)
	b := NewBarrier(ctx, sw.Output(0))

	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := b.SendInput(ctx, input, outputCh)
	testifyassert.NoError(t, err)
	testifyassert.Len(t, outputCh, 1)
}

func TestBarrier_Drop(t *testing.T) {
	ctx := context.Background()
	sw := barrierstategetter.NewSwitch()
	sw.CurrentValue.Store(1) // Output 0 is not active — will drop
	b := NewBarrier(ctx, sw.Output(0))

	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := b.SendInput(ctx, input, outputCh)
	// Drop produces ErrSkip which is handled by the boilerplate
	if err != nil {
		var skipErr boilerplate.ErrSkip
		testifyassert.True(t, errors.As(err, &skipErr))
	}
}

func TestBarrier_Block_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sw := barrierstategetter.NewSwitch()
	sw.CurrentValue.Store(1)
	sw.Flags.Set(barrierstategetter.SwitchFlagNextOutputStateBlock)
	b := NewBarrier(ctx, sw.Output(0))

	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)

	done := make(chan error, 1)
	go func() {
		done <- b.SendInput(ctx, input, outputCh)
	}()

	// Cancel to unblock
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Should get context cancelled or ErrSkip
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("barrier did not unblock on context cancel")
	}
}

// --- Retryable kernel tests ---

func TestRetryable_NewRetryable(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	testifyassert.NotNil(t, r)
	testifyassert.False(t, r.KernelIsSet)
}

func TestRetryable_String(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	s := r.String()
	testifyassert.Contains(t, s, "Retry")
}

func TestRetryable_GetObjectID(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	id := r.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestRetryable_IsPaused(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	testifyassert.True(t, r.IsPaused(ctx))
}

func TestRetryable_UnpauseAndPause(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)

	err := r.Unpause(ctx)
	testifyassert.NoError(t, err)
	// After unpause, IsPaused should return false
	time.Sleep(50 * time.Millisecond)
	testifyassert.False(t, r.IsPaused(ctx))
}

func TestRetryable_Close(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	err := r.Close(ctx)
	testifyassert.NoError(t, err)
}

func TestRetryable_OriginalPacketSource_NotSet(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return &Dummy{}, nil
		},
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	src := r.OriginalPacketSource()
	testifyassert.Nil(t, src)
}

func TestRetryable_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*Retryable[Abstract])(nil)
}

// --- RetryableOption tests ---

func TestRetryableOptions_Config_Default(t *testing.T) {
	cfg := RetryableOptions[Abstract](nil).Config()
	testifyassert.True(t, cfg.StartOnInit)
	testifyassert.Nil(t, cfg.OnInit)
	testifyassert.Nil(t, cfg.OnKernelOpen)
}

func TestRetryableOptionStartOnInit(t *testing.T) {
	cfg := RetryableOptions[Abstract]{
		RetryableOptionStartOnInit[Abstract](false),
	}.Config()
	testifyassert.False(t, cfg.StartOnInit)
}

func TestRetryableOptionOnInit(t *testing.T) {
	called := false
	cfg := RetryableOptions[Abstract]{
		RetryableOptionOnInit[Abstract](func(ctx context.Context, r *Retryable[Abstract]) {
			called = true
		}),
		RetryableOptionStartOnInit[Abstract](false),
	}.Config()
	testifyassert.NotNil(t, cfg.OnInit)
	cfg.OnInit(context.Background(), nil)
	testifyassert.True(t, called)
}

func TestRetryableOptionOnPreKernelOpen(t *testing.T) {
	cfg := RetryableOptions[Abstract]{
		RetryableOptionOnPreKernelOpen[Abstract](func(ctx context.Context, r *Retryable[Abstract]) error {
			return nil
		}),
	}.Config()
	testifyassert.NotNil(t, cfg.OnPreKernelOpen)
}

func TestRetryableOptionOnKernelOpen(t *testing.T) {
	called := false
	cfg := RetryableOptions[Abstract]{
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			called = true
			return nil
		}),
	}.Config()
	testifyassert.NotNil(t, cfg.OnKernelOpen)
	err := cfg.OnKernelOpen(context.Background(), nil)
	testifyassert.NoError(t, err)
	testifyassert.True(t, called)
}

// --- Barrier state tests ---

func TestBarrierStates(t *testing.T) {
	testifyassert.Equal(t, barriertypes.StatePass, barrierstategetter.StatePass)
	testifyassert.Equal(t, barriertypes.StateDrop, barrierstategetter.StateDrop)
	testifyassert.Equal(t, barriertypes.StateBlock, barrierstategetter.StateBlock)
}
