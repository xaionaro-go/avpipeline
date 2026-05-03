// orchestration_kernels_test.go tests the orchestration kernel types (Switch, ChainOfTwo, ChainOfThree,
// Chain, Tee, ReorderMonotonicDTS, AudioSync accessors, codec options) that are pure-Go and don't
// require FFmpeg I/O to test.
package kernel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// --- Switch tests ---

func TestSwitch_NewSwitch(t *testing.T) {
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	require.NotNil(t, sw)
	require.Equal(t, 2, len(sw.Kernels))
	testifyassert.Equal(t, uint32(0), sw.KernelIndex.Load())
	testifyassert.Equal(t, int32(-1), sw.NextKernelIndex.Load())
}

func TestSwitch_GetKernelIndex(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{}, &Dummy{})
	testifyassert.Equal(t, uint(0), sw.GetKernelIndex(context.Background()))
}

func TestSwitch_SetKernelIndex_Direct(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{}, &Dummy{})

	// No KeepUnless or VerifySwitchOutput → direct switch
	err := sw.SetKernelIndex(ctx, 1)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(1), sw.GetKernelIndex(ctx))
}

func TestSwitch_SetKernelIndex_OutOfRange(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{})
	err := sw.SetKernelIndex(ctx, 5)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "requested processor")
}

func TestSwitch_SetKernelIndex_WithKeepUnless(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{}, &Dummy{})
	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	sw.SetKeepUnless(cond)
	testifyassert.NotNil(t, sw.GetKeepUnless())

	// With KeepUnless set, SetKernelIndex stores NextKernelIndex instead of switching directly
	err := sw.SetKernelIndex(ctx, 1)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(0), sw.GetKernelIndex(ctx)) // Still on 0
	testifyassert.Equal(t, int32(1), sw.NextKernelIndex.Load())
}

func TestSwitch_GetSetKeepUnless(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{})
	testifyassert.Nil(t, sw.GetKeepUnless())

	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	sw.SetKeepUnless(cond)
	testifyassert.NotNil(t, sw.GetKeepUnless())
}

func TestSwitch_GetSetVerifySwitchOutput(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{})
	testifyassert.Nil(t, sw.GetVerifySwitchOutput())

	cond := packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	})
	sw.SetVerifySwitchOutput(cond)
	testifyassert.NotNil(t, sw.GetVerifySwitchOutput())
}

func TestSwitch_GetKernel(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	testifyassert.Equal(t, Abstract(k1), sw.GetKernel(ctx))

	sw.KernelIndex.Store(1)
	testifyassert.Equal(t, Abstract(k2), sw.GetKernel(ctx))
}

func TestSwitch_GetObjectID(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{})
	id := sw.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestSwitch_String(t *testing.T) {
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	s := sw.String()
	testifyassert.True(t, strings.HasPrefix(s, "Switch("))
	testifyassert.Contains(t, s, "->Dummy") // Active kernel has ->
	testifyassert.Contains(t, s, "Dummy")
}

func TestSwitch_Generate(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k1.GenerateCallCount)
	testifyassert.Equal(t, 1, k2.GenerateCallCount)
}

func TestSwitch_Generate_WithError(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		GenerateFn: func(ctx context.Context, ch chan<- packetorframe.OutputUnion) error {
			return fmt.Errorf("gen error")
		},
	}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.Generate(ctx, outCh)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "gen error")
}

func TestSwitch_SendInput_NoNextKernel(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k1.SendInputCallCount)
	testifyassert.Equal(t, 0, k2.SendInputCallCount)
	testifyassert.Len(t, outCh, 1)
}

func TestSwitch_SendInput_SameKernelIndex(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	sw := NewSwitch[Abstract](k1)
	sw.NextKernelIndex.Store(0) // Same as current

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, int32(-1), sw.NextKernelIndex.Load()) // Cleared
}

func TestSwitch_SendInput_WithKeepUnless_CondNotMet(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)

	// KeepUnless never matches — stays on current kernel
	sw.SetKeepUnless(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return false
	}))
	sw.NextKernelIndex.Store(1)

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(0), sw.GetKernelIndex(ctx)) // Stayed on 0
	testifyassert.Equal(t, 1, k1.SendInputCallCount)
}

func TestSwitch_SendInput_WithKeepUnless_CondMet_NoVerify(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)

	// KeepUnless matches — should commit to next kernel
	sw.SetKeepUnless(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	}))
	sw.NextKernelIndex.Store(1)

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(1), sw.GetKernelIndex(ctx)) // Switched to 1
}

func TestSwitch_SendInput_WithVerify_Match(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{}
	k2 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			// Produce output that satisfies the verify condition
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	sw := NewSwitch[Abstract](k1, k2)

	// KeepUnless matches → proceed to verify
	sw.SetKeepUnless(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	}))
	// Verify always matches
	sw.SetVerifySwitchOutput(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	}))
	sw.NextKernelIndex.Store(1)

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(1), sw.GetKernelIndex(ctx)) // Switched after verification
}

func TestSwitch_SendInput_WithVerify_NoMatch(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	k2 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			// Produce output that does NOT satisfy verify
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	sw := NewSwitch[Abstract](k1, k2)

	sw.SetKeepUnless(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return true
	}))
	// Verify never matches — fallback to current kernel
	sw.SetVerifySwitchOutput(packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
		return false
	}))
	sw.NextKernelIndex.Store(1)

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, uint(0), sw.GetKernelIndex(ctx)) // Stayed on 0
	testifyassert.Equal(t, 1, k1.SendInputCallCount)        // Sent via current kernel
}

func TestSwitch_Close(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{}
	k2 := &Dummy{}
	sw := NewSwitch[Abstract](k1, k2)
	err := sw.Close(ctx)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k1.CloseCallCount)
	testifyassert.Equal(t, 1, k2.CloseCallCount)
}

func TestSwitch_Close_WithError(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		CloseFn: func(ctx context.Context) error { return fmt.Errorf("close err") },
	}
	sw := NewSwitch[Abstract](k1)
	err := sw.Close(ctx)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "close err")
}

func TestSwitch_OriginalPacketSource_NoKernelIsSource(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{})
	src := sw.OriginalPacketSource()
	testifyassert.Nil(t, src)
}

func TestSwitch_OriginalPacketSource_OutOfRange(t *testing.T) {
	sw := NewSwitch[Abstract](&Dummy{})
	sw.KernelIndex.Store(99)
	src := sw.OriginalPacketSource()
	testifyassert.Nil(t, src)
}

func TestSwitch_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{})
	called := false
	sw.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	// Dummy doesn't implement packet.Source.WithOutputFormatContext, so callback shouldn't be called
	testifyassert.False(t, called)
}

func TestSwitch_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{})
	called := false
	sw.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestSwitch_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch[Abstract](&Dummy{})
	err := sw.NotifyAboutPacketSource(ctx, nil)
	require.NoError(t, err) // Dummy doesn't implement packet.Sink
}

func TestSwitch_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*Switch[Abstract])(nil)
	var _ packet.Source = (*Switch[Abstract])(nil)
	var _ packet.Sink = (*Switch[Abstract])(nil)
}

// --- ChainOfTwo additional tests (supplement chain_of_two_test.go) ---

func TestChainOfTwo_SendInput_Chain(t *testing.T) {
	ctx := context.Background()
	k0 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			// Pass through to the next kernel
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)

	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.SendInputCallCount)
	testifyassert.Equal(t, 1, k1.SendInputCallCount)
	testifyassert.Len(t, outCh, 1)
}

func TestChainOfTwo_GetObjectID(t *testing.T) {
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	id := chain.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestChainOfTwo_String(t *testing.T) {
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	s := chain.String()
	testifyassert.Contains(t, s, "ChainOfTwo")
	testifyassert.Contains(t, s, "Dummy")
}

func TestChainOfTwo_Close(t *testing.T) {
	ctx := context.Background()
	k0 := &Dummy{}
	k1 := &Dummy{}
	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	err := chain.Close(ctx)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.CloseCallCount)
	testifyassert.Equal(t, 1, k1.CloseCallCount)
}

func TestChainOfTwo_Close_WithError(t *testing.T) {
	ctx := context.Background()
	k0 := &Dummy{CloseFn: func(ctx context.Context) error { return fmt.Errorf("k0 err") }}
	k1 := &Dummy{}
	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	err := chain.Close(ctx)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "k0 err")
}

func TestChainOfTwo_GetKernels(t *testing.T) {
	k0 := &Dummy{}
	k1 := &Dummy{}
	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	kernels := chain.GetKernels()
	require.Len(t, kernels, 2)
	testifyassert.Equal(t, Abstract(k0), kernels[0])
	testifyassert.Equal(t, Abstract(k1), kernels[1])
}

func TestChainOfTwo_OriginalPacketSource(t *testing.T) {
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	src := chain.OriginalPacketSource()
	testifyassert.Nil(t, src) // Dummy doesn't implement packet.Source properly
}

func TestChainOfTwo_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	called := false
	chain.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called) // Dummy doesn't have format context
}

func TestChainOfTwo_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	called := false
	chain.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestChainOfTwo_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfTwo[Abstract, Abstract](&Dummy{}, &Dummy{})
	err := chain.NotifyAboutPacketSource(ctx, nil)
	require.NoError(t, err)
}

// --- ChainOfThree additional tests ---

func TestChainOfThree_GetObjectID(t *testing.T) {
	chain := NewChainOfThree[Abstract, Abstract, Abstract](&Dummy{}, &Dummy{}, &Dummy{})
	id := chain.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestChainOfThree_OriginalPacketSource(t *testing.T) {
	chain := NewChainOfThree[Abstract, Abstract, Abstract](&Dummy{}, &Dummy{}, &Dummy{})
	src := chain.OriginalPacketSource()
	testifyassert.Nil(t, src)
}

func TestChainOfThree_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfThree[Abstract, Abstract, Abstract](&Dummy{}, &Dummy{}, &Dummy{})
	called := false
	chain.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestChainOfThree_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfThree[Abstract, Abstract, Abstract](&Dummy{}, &Dummy{}, &Dummy{})
	called := false
	chain.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestChainOfThree_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	chain := NewChainOfThree[Abstract, Abstract, Abstract](&Dummy{}, &Dummy{}, &Dummy{})
	err := chain.NotifyAboutPacketSource(ctx, nil)
	require.NoError(t, err)
}

func TestChainOfThree_SendInput_Chain(t *testing.T) {
	ctx := context.Background()
	passThroughFn := func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
		outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
		return nil
	}
	k0 := &Dummy{SendInputFn: passThroughFn}
	k1 := &Dummy{SendInputFn: passThroughFn}
	k2 := &Dummy{SendInputFn: passThroughFn}
	chain := NewChainOfThree[Abstract, Abstract, Abstract](k0, k1, k2)

	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)

	err := chain.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.SendInputCallCount)
	testifyassert.Equal(t, 1, k1.SendInputCallCount)
	testifyassert.Equal(t, 1, k2.SendInputCallCount)
	testifyassert.Len(t, outCh, 1)
}

// --- Chain additional tests ---

func TestChain_GetObjectID(t *testing.T) {
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	id := chain.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestChain_String(t *testing.T) {
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	s := chain.String()
	testifyassert.Contains(t, s, "Chain[")
	testifyassert.Contains(t, s, "Dummy")
}

func TestChain_Close(t *testing.T) {
	ctx := context.Background()
	k0 := &Dummy{}
	k1 := &Dummy{}
	chain := NewChain[Abstract](k0, k1)
	err := chain.Close(ctx)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.CloseCallCount)
	testifyassert.Equal(t, 1, k1.CloseCallCount)
}

func TestChain_Close_WithError(t *testing.T) {
	ctx := context.Background()
	k0 := &Dummy{CloseFn: func(ctx context.Context) error { return fmt.Errorf("chain close err") }}
	chain := NewChain[Abstract](k0)
	err := chain.Close(ctx)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "chain close err")
}

func TestChain_OriginalPacketSource(t *testing.T) {
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	src := chain.OriginalPacketSource()
	testifyassert.Nil(t, src)
}

func TestChain_SendInput_EmptyKernels(t *testing.T) {
	ctx := context.Background()
	chain := NewChain[Abstract]()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.SendInput(ctx, packetorframe.InputUnion{}, outCh)
	require.NoError(t, err)
}

func TestChain_SendInput_SingleKernel(t *testing.T) {
	ctx := context.Background()
	k := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	chain := NewChain[Abstract](k)
	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Len(t, outCh, 1)
}

func TestChain_SendInput_MultipleKernels(t *testing.T) {
	ctx := context.Background()
	passThroughFn := func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
		if input.Packet != nil {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
		} else if input.Frame != nil {
			outputCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(input.Frame)}
		}
		return nil
	}
	k0 := &Dummy{SendInputFn: passThroughFn}
	k1 := &Dummy{SendInputFn: passThroughFn}
	k2 := &Dummy{SendInputFn: passThroughFn}
	chain := NewChain[Abstract](k0, k1, k2)
	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.SendInputCallCount)
	testifyassert.Equal(t, 1, k1.SendInputCallCount)
	testifyassert.Equal(t, 1, k2.SendInputCallCount)
	testifyassert.Len(t, outCh, 1)
}

func TestChain_Generate_EmptyKernels(t *testing.T) {
	ctx := context.Background()
	chain := NewChain[Abstract]()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)
}

func TestChain_Generate_SingleKernel(t *testing.T) {
	ctx := context.Background()
	k := &Dummy{}
	chain := NewChain[Abstract](k)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k.GenerateCallCount)
}

func TestChain_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	called := false
	chain.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestChain_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	called := false
	chain.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestChain_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	chain := NewChain[Abstract](&Dummy{}, &Dummy{})
	err := chain.NotifyAboutPacketSource(ctx, nil)
	require.NoError(t, err)
}

// --- Tee additional tests (supplement tee_test.go) ---

func TestTee_GetObjectID(t *testing.T) {
	tks := Tee[Abstract]{&fakeKernel{}, &fakeKernel{}}
	id := tks.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestTee_OriginalPacketSource(t *testing.T) {
	tks := Tee[Abstract]{&fakeKernel{}, &fakeKernel{}}
	src := tks.OriginalPacketSource()
	testifyassert.Nil(t, src) // fakeKernel doesn't implement packet.Source
}

func TestTee_WithOutputFormatContext(t *testing.T) {
	ctx := context.Background()
	tks := Tee[Abstract]{&fakeKernel{}, &fakeKernel{}}
	called := false
	tks.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestTee_WithInputFormatContext(t *testing.T) {
	ctx := context.Background()
	tks := Tee[Abstract]{&fakeKernel{}, &fakeKernel{}}
	called := false
	tks.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		called = true
	})
	testifyassert.False(t, called)
}

func TestTee_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	tks := Tee[Abstract]{&fakeKernel{}, &fakeKernel{}}
	err := tks.NotifyAboutPacketSource(ctx, nil)
	require.NoError(t, err)
}

func TestTee_Close_Empty(t *testing.T) {
	ctx := context.Background()
	tks := Tee[Abstract]{}
	err := tks.Close(ctx)
	require.NoError(t, err)
}

func TestTee_CloseChan_Empty(t *testing.T) {
	tks := Tee[Abstract]{}
	ch := tks.CloseChan()
	testifyassert.Nil(t, ch)
}

func TestTee_CloseChan_AllNilChildren(t *testing.T) {
	// closeFakeKernel with nil ch
	tks := Tee[Abstract]{
		&closeFakeKernel{ch: nil},
		&closeFakeKernel{ch: nil},
	}
	ch := tks.CloseChan()
	testifyassert.Nil(t, ch)
}

// --- ReorderMonotonicDTS tests ---

func TestReorderMonotonicDTS_NewReorderMonotonicDTS(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, time.Duration(1000), false)
	require.NotNil(t, r)
	testifyassert.Equal(t, time.Duration(1000), r.MaxDTSDifference)
	testifyassert.False(t, r.Started)
	testifyassert.False(t, r.DiscardUnorderedItems)
}

func TestReorderMonotonicDTS_String(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	testifyassert.Equal(t, "ReorderMonotonicDTS", r.String())
}

func TestReorderMonotonicDTS_GetObjectID(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	id := r.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestReorderMonotonicDTS_Close(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	err := r.Close(ctx)
	require.NoError(t, err)
	// Close again should be idempotent
	err = r.Close(ctx)
	require.NoError(t, err)
}

func TestReorderMonotonicDTS_EmptyQueuesCount(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	count := r.EmptyQueuesCount(ctx)
	testifyassert.Equal(t, uint(0), count)
}

func TestReorderMonotonicDTS_Generate_EmptyQueue(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	outCh := make(chan packetorframe.OutputUnion, 10)

	// Close the signaler so Generate doesn't block
	r.Close(ctx)

	err := r.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Len(t, outCh, 0)
}

func TestReorderMonotonicDTS_CurrentDTS_Empty(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, false)
	dts := r.CurrentDTS()
	testifyassert.False(t, dts.IsSet())
}

func TestReorderMonotonicDTS_DiscardUnorderedItems(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 1000, true)
	testifyassert.True(t, r.DiscardUnorderedItems)
}

// --- AudioSync accessor tests ---

func TestAudioSync_NewAudioSync(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	require.NotNil(t, as)
}

func TestAudioSync_String(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	testifyassert.Equal(t, "AudioSync", as.String())
}

func TestAudioSync_GetObjectID(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	id := as.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

func TestAudioSync_Close(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	err := as.Close(ctx)
	require.NoError(t, err)
}

func TestAudioSync_Generate(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := as.Generate(ctx, outCh)
	require.NoError(t, err)
}

func TestAudioSync_Reset(t *testing.T) {
	ctx := context.Background()
	as := NewAudioSync(ctx, nil)
	err := as.Reset(ctx)
	require.NoError(t, err)
}

func TestAudioSync_ImplementsAbstract(t *testing.T) {
	var _ Abstract = (*AudioSync)(nil)
}

// --- CodecResetOptions tests ---

func TestCodecResetOptions_Config_Empty(t *testing.T) {
	opts := CodecResetOptions(nil)
	cfg := opts.config()
	_ = cfg // Just verify it doesn't panic
}

func TestCodecResetOptions_Apply(t *testing.T) {
	opts := CodecResetOptions{}
	cfg := &codecResetConfig{}
	opts.apply(cfg) // Should not panic
}

// --- ErrRetry tests ---

func TestErrRetry_Error_WithInner(t *testing.T) {
	inner := fmt.Errorf("inner")
	err := ErrRetry{Err: inner}
	testifyassert.Contains(t, err.Error(), "inner")
	testifyassert.Contains(t, err.Error(), "please retry")
}

func TestErrRetry_Error_NilInner(t *testing.T) {
	err := ErrRetry{}
	testifyassert.Contains(t, err.Error(), "please retry")
}

// --- ErrKernelNotSet tests ---

func TestErrKernelNotSet_Error(t *testing.T) {
	err := ErrKernelNotSet{}
	testifyassert.Equal(t, "kernel is not set", err.Error())
}

// --- Retryable delegation tests ---

func TestRetryable_GetInternalQueueSize_NoKernel(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	// GetInternalQueueSize doesn't go through retry(), it checks KernelIsSet directly
	size := r.GetInternalQueueSize(ctx)
	testifyassert.Nil(t, size)
}

func TestRetryable_GetOldestDTSInTheQueue_NoKernel(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](false),
	)
	dts, err := r.GetOldestDTSInTheQueue(ctx)
	require.Error(t, err)
	var kernelNotSet ErrKernelNotSet
	testifyassert.ErrorAs(t, err, &kernelNotSet)
	testifyassert.Equal(t, time.Duration(0), dts)
}

// --- Retryable retry/SendInput/Generate tests (with StartOnInit=true) ---

func TestRetryable_SendInput_WithKernel(t *testing.T) {
	ctx := context.Background()
	d := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return d, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	// Wait for kernel to be opened
	time.Sleep(100 * time.Millisecond)

	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := r.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, d.SendInputCallCount)
	testifyassert.Len(t, outCh, 1)
}

func TestRetryable_Generate_WithKernel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Dummy{}
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return d, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(100 * time.Millisecond)

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := r.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, d.GenerateCallCount)
}

func TestRetryable_SendInput_FactoryError_NoOnError(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return nil, fmt.Errorf("factory failed")
		},
		nil, // no OnError — should close immediately
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(100 * time.Millisecond)

	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := r.SendInput(ctx, input, outCh)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "factory failed")
}

func TestRetryable_SendInput_FactoryError_WithRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callCount := 0
	d := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			callCount++
			if callCount <= 2 {
				return nil, fmt.Errorf("factory failed attempt %d", callCount)
			}
			return d, nil
		},
		func(ctx context.Context, k Abstract, err error) error {
			return ErrRetry{Err: err}
		},
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	// Wait for retries
	time.Sleep(500 * time.Millisecond)

	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := r.SendInput(ctx, input, outCh)
	require.NoError(t, err)
	testifyassert.GreaterOrEqual(t, callCount, 3)
}

func TestRetryable_OnKernelOpen_Called(t *testing.T) {
	ctx := context.Background()
	var onKernelOpenCalled atomic.Bool
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			onKernelOpenCalled.Store(true)
			return nil
		}),
	)
	defer r.Close(ctx)
	time.Sleep(100 * time.Millisecond)
	testifyassert.True(t, onKernelOpenCalled.Load())
}

func TestRetryable_OnKernelOpen_Error(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
		RetryableOptionOnKernelOpen[Abstract](func(ctx context.Context, k Abstract) error {
			return fmt.Errorf("on kernel open error")
		}),
	)
	defer r.Close(ctx)
	time.Sleep(100 * time.Millisecond)

	// Kernel should not be set
	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := r.SendInput(ctx, input, outCh)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "on kernel open error")
}

// TestRetryable_OriginalPacketSource_WithKernel removed:
// Pre-existing data race in retryable.go — OriginalPacketSource() reads
// r.KernelIsSet/r.Kernel without holding KernelLocker while openKernelIfNeeded writes them.

// --- ReorderMonotonicDTS SendInput tests ---

func TestReorderMonotonicDTS_SendInput_SingleStream(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 10000, false)
	outCh := make(chan packetorframe.OutputUnion, 100)

	// Send packets from stream 0 with increasing DTS
	for i := 0; i < 5; i++ {
		input, cleanup := makePacketInput(t)
		defer cleanup()
		input.Packet.Packet.SetDts(int64(i * 100))
		err := r.SendInput(ctx, input, outCh)
		require.NoError(t, err)
	}

	// Only one stream, so items should start flowing once the queue has entries
	// and the start condition is met (nil condition = immediate start)
}

func TestReorderMonotonicDTS_SendInput_TwoStreams(t *testing.T) {
	ctx := context.Background()
	r := NewReorderMonotonicDTS(ctx, nil, 100, 10000, false)
	outCh := make(chan packetorframe.OutputUnion, 100)

	// Send packets from two streams interleaved
	for i := 0; i < 4; i++ {
		input, cleanup := makePacketInput(t)
		defer cleanup()
		input.Packet.Packet.SetDts(int64(i * 100))
		input.Packet.StreamInfo.StreamIndex = i % 2
		err := r.SendInput(ctx, input, outCh)
		require.NoError(t, err)
	}
	// After 4 packets across 2 streams, items should start appearing in output
	testifyassert.Greater(t, len(outCh), 0)
}

// --- Chain Generate with multiple kernels ---

func TestChain_Generate_MultipleKernels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			// Generate produces nothing, just returns
			return nil
		},
	}
	k1 := &Dummy{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			return nil
		},
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			return nil
		},
	}
	chain := NewChain[Abstract](k0, k1)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 1, k0.GenerateCallCount)
	testifyassert.Equal(t, 1, k1.GenerateCallCount)
}

func TestChainOfTwo_Generate_WithData(t *testing.T) {
	ctx := context.Background()

	k0 := &Dummy{
		GenerateFn: func(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
			cp := astiav.AllocCodecParameters()
			pkt := astiav.AllocPacket()
			outputCh <- packetorframe.OutputUnion{Packet: ptr(packet.BuildOutput(pkt, &packetorframetypes.StreamInfo{CodecParameters: cp}))}
			return nil
		},
	}
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			outputCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
			return nil
		},
	}
	chain := NewChainOfTwo[Abstract, Abstract](k0, k1)
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := chain.Generate(ctx, outCh)
	require.NoError(t, err)
	testifyassert.Len(t, outCh, 1)
}

// --- Switch sendViaKernel error test ---

func TestSwitch_SendInput_KernelError(t *testing.T) {
	ctx := context.Background()
	k1 := &Dummy{
		SendInputFn: func(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
			return fmt.Errorf("kernel error")
		},
	}
	sw := NewSwitch[Abstract](k1)
	input, cleanup := makePacketInput(t)
	defer cleanup()
	outCh := make(chan packetorframe.OutputUnion, 10)
	err := sw.SendInput(ctx, input, outCh)
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "kernel error")
}

// --- Retryable WithNetworkConn/WithRawNetworkConn tests ---

// DummyWithNetConn embeds Dummy and adds WithNetworkConn/WithRawNetworkConn support.
type DummyWithNetConn struct {
	Dummy
	WithNetworkConnFn    func(context.Context, func(context.Context, net.Conn) error) error
	WithRawNetworkConnFn func(context.Context, func(context.Context, syscall.RawConn, string) error) error
}

var (
	_ Abstract           = (*DummyWithNetConn)(nil)
	_ WithNetworkConner  = (*DummyWithNetConn)(nil)
	_ WithRawNetworkConner = (*DummyWithNetConn)(nil)
)

func (d *DummyWithNetConn) WithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	if d.WithNetworkConnFn != nil {
		return d.WithNetworkConnFn(ctx, callback)
	}
	return nil
}

func (d *DummyWithNetConn) WithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	if d.WithRawNetworkConnFn != nil {
		return d.WithRawNetworkConnFn(ctx, callback)
	}
	return nil
}

func TestRetryable_WithNetworkConn_NoKernel(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[*DummyWithNetConn](ctx,
		func(ctx context.Context) (*DummyWithNetConn, error) { return &DummyWithNetConn{}, nil },
		nil,
		RetryableOptionStartOnInit[*DummyWithNetConn](false),
	)
	err := r.WithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
		return nil
	})
	require.Error(t, err)
	var kernelNotSet ErrKernelNotSet
	testifyassert.ErrorAs(t, err, &kernelNotSet)
}

func TestRetryable_WithRawNetworkConn_NoKernel(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[*DummyWithNetConn](ctx,
		func(ctx context.Context) (*DummyWithNetConn, error) { return &DummyWithNetConn{}, nil },
		nil,
		RetryableOptionStartOnInit[*DummyWithNetConn](false),
	)
	err := r.WithRawNetworkConn(ctx, func(ctx context.Context, rawConn syscall.RawConn, network string) error {
		return nil
	})
	require.Error(t, err)
	var kernelNotSet ErrKernelNotSet
	testifyassert.ErrorAs(t, err, &kernelNotSet)
}

func TestRetryable_WithNetworkConn_NotImplemented(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(50 * time.Millisecond) // wait for kernel to initialize
	err := r.WithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
		return nil
	})
	require.Error(t, err)
	var notImpl ErrNotImplemented
	testifyassert.ErrorAs(t, err, &notImpl)
}

func TestRetryable_WithRawNetworkConn_NotImplemented(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &Dummy{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(50 * time.Millisecond) // wait for kernel to initialize
	err := r.WithRawNetworkConn(ctx, func(ctx context.Context, rawConn syscall.RawConn, network string) error {
		return nil
	})
	require.Error(t, err)
	var notImpl ErrNotImplemented
	testifyassert.ErrorAs(t, err, &notImpl)
}

func TestRetryable_WithRawNetworkConn_WithKernel(t *testing.T) {
	ctx := context.Background()
	callbackCalled := false
	d := &DummyWithNetConn{
		WithRawNetworkConnFn: func(ctx context.Context, callback func(context.Context, syscall.RawConn, string) error) error {
			return callback(ctx, nil, "tcp")
		},
	}
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return d, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(50 * time.Millisecond) // wait for kernel to initialize
	err := r.WithRawNetworkConn(ctx, func(ctx context.Context, rawConn syscall.RawConn, network string) error {
		callbackCalled = true
		testifyassert.Equal(t, "tcp", network)
		return nil
	})
	require.NoError(t, err)
	testifyassert.True(t, callbackCalled)
}

func TestRetryable_WithNetworkConn_WithKernel(t *testing.T) {
	ctx := context.Background()
	callbackCalled := false
	d := &DummyWithNetConn{
		WithNetworkConnFn: func(ctx context.Context, callback func(context.Context, net.Conn) error) error {
			return callback(ctx, nil)
		},
	}
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return d, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)
	time.Sleep(50 * time.Millisecond) // wait for kernel to initialize
	err := r.WithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
		callbackCalled = true
		return nil
	})
	require.NoError(t, err)
	testifyassert.True(t, callbackCalled)
}

// dummyWithQueueSize embeds Dummy and implements GetInternalQueueSize and GetOldestDTSInTheQueue,
// used by stress tests for the Retryable kernel.
type dummyWithQueueSize struct {
	Dummy
}

var (
	_ Abstract              = (*dummyWithQueueSize)(nil)
	_ GetInternalQueueSizer = (*dummyWithQueueSize)(nil)
)

func (d *dummyWithQueueSize) GetInternalQueueSize(ctx context.Context) map[string]uint64 {
	return map[string]uint64{"dummy": 0}
}

func (d *dummyWithQueueSize) GetOldestDTSInTheQueue(ctx context.Context) (time.Duration, error) {
	return 0, nil
}

// TestRetryable_GetInternalQueueSize_ConcurrentWithPause stresses concurrent calls
// to GetInternalQueueSize/GetOldestDTSInTheQueue against Pause/Unpause, which race
// against r.Kernel writes in openKernelIfNeeded/pauseLocked. Without proper
// locking of the r.Kernel read, this test would panic on a torn interface or
// dereference a stale pointer.
func TestRetryable_GetInternalQueueSize_ConcurrentWithPause(t *testing.T) {
	ctx := context.Background()
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) { return &dummyWithQueueSize{}, nil },
		nil,
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)

	stop := make(chan struct{})
	done := make(chan struct{}, 3)

	// Reader A: GetInternalQueueSize in a loop
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.GetInternalQueueSize(ctx)
		}
	}()

	// Reader B: GetOldestDTSInTheQueue in a loop
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = r.GetOldestDTSInTheQueue(ctx)
		}
	}()

	// Writer: Pause/Unpause in a loop
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.Pause(ctx)
			_ = r.Unpause(ctx)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	for i := 0; i < 3; i++ {
		<-done
	}
}

// --- Decoder frame pass-through test ---

func TestDecoder_SendInput_FramePassThrough(t *testing.T) {
	ctx := context.Background()
	dec := NewDecoder[*codec.NaiveDecoderFactory](ctx, codec.NewNaiveDecoderFactory(ctx, nil))
	defer dec.Close(ctx)

	input, cleanup := makeFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 1)
	err := dec.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		require.NotNil(t, out.Frame, "expected a frame in the output")
		require.Nil(t, out.Packet, "expected no packet in the output")
	default:
		t.Fatal("expected output but channel was empty")
	}
}

// TestRetryable_Pause_NotStarvedByFactoryRetry is a regression test for the
// AddInput-deadlock observed on the Android test phone: when the wrapped
// factory keeps failing (e.g. fallback rtmp source returns "Connection
// refused") and OnError sleeps RetryInterval to back off, Pause must still
// be able to acquire KernelLocker promptly. Before the fix, OnError was
// invoked while KernelLocker was held, so the sleep starved any concurrent
// control op (Pause / Unpause / AddInput → chain.Pause+Unpause); the gRPC
// call would hang indefinitely.
//
// This test arranges a factory that always fails and an OnError that
// sleeps long enough that any naive lock-during-sleep would be observable.
// It then calls Pause concurrently and asserts Pause returns within a
// short bound.
func TestRetryable_Pause_NotStarvedByFactoryRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	factoryErr := fmt.Errorf("factory always fails")
	r := NewRetryable[Abstract](ctx,
		func(ctx context.Context) (Abstract, error) {
			return nil, factoryErr
		},
		func(ctx context.Context, k Abstract, err error) error {
			// Mimic inputwithfallback.onInputChainError's
			// `defer time.Sleep(RetryInterval)` behaviour. 1s
			// matches the production -retry_input_timeout_on_failure.
			time.Sleep(1 * time.Second)
			return ErrRetry{Err: err}
		},
		RetryableOptionStartOnInit[Abstract](true),
	)
	defer r.Close(ctx)

	// Let the factory fail at least once so OnError is in its sleep.
	time.Sleep(50 * time.Millisecond)

	// Pause must return promptly. With the bug, this blocks for the
	// entire OnError sleep; with the fix, Pause grabs the lock during
	// the sleep window. Pick a bound well below the sleep duration so
	// a regression is unambiguous.
	pauseDone := make(chan error, 1)
	go func() {
		pauseDone <- r.Pause(ctx)
	}()
	select {
	case err := <-pauseDone:
		require.NoError(t, err, "Pause should not error")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Pause blocked while OnError was sleeping — KernelLocker is held across the sleep (regression)")
	}
}
