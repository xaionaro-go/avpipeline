package stategetter

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

func TestGetValue(t *testing.T) {
	sw := NewSwitch()
	ctx := context.Background()

	assert.Equal(t, int32(0), sw.GetValue(ctx))

	sw.CurrentValue.Store(42)
	assert.Equal(t, int32(42), sw.GetValue(ctx))
}

func TestSwitchOutput_String(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(1)
	out := sw.Output(1)
	s := out.String()
	assert.Contains(t, s, "SwitchOutput(true")
	assert.Contains(t, s, "cur:1")

	out2 := sw.Output(2)
	s2 := out2.String()
	assert.Contains(t, s2, "SwitchOutput(false")
}

func TestGetSetKeepUnless(t *testing.T) {
	sw := NewSwitch()

	// Initially nil
	assert.Nil(t, sw.GetKeepUnless())

	// Set a condition
	cond := packetorframecondition.Static(true)
	sw.SetKeepUnless(cond)
	got := sw.GetKeepUnless()
	require.NotNil(t, got)
}

func TestGetSetOnSwitchRequest(t *testing.T) {
	sw := NewSwitch()

	assert.Nil(t, sw.GetOnSwitchRequest())

	fn := FuncOnSwitchRequest(func(ctx context.Context, pkt packetorframe.InputUnion, to int32) error {
		return nil
	})
	sw.SetOnSwitchRequest(fn)
	assert.NotNil(t, sw.GetOnSwitchRequest())
}

func TestGetSetOnBeforeSwitch(t *testing.T) {
	sw := NewSwitch()

	assert.Nil(t, sw.GetOnBeforeSwitch())

	fn := FuncOnBeforeSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {})
	sw.SetOnBeforeSwitch(fn)
	assert.NotNil(t, sw.GetOnBeforeSwitch())
}

func TestGetSetOnInterruptedSwitch(t *testing.T) {
	sw := NewSwitch()

	assert.Nil(t, sw.GetOnInterruptedSwitch())

	fn := FuncOnInterruptedSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {})
	sw.SetOnInterruptedSwitch(fn)
	assert.NotNil(t, sw.GetOnInterruptedSwitch())
}

func TestGetSetOnAfterSwitch(t *testing.T) {
	sw := NewSwitch()

	assert.Nil(t, sw.GetOnAfterSwitch())

	fn := FuncOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {})
	sw.SetOnAfterSwitch(fn)
	assert.NotNil(t, sw.GetOnAfterSwitch())
}

func TestOnSwitchRequest_Error(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	ctx := context.Background()

	sw.SetOnSwitchRequest(func(ctx context.Context, pkt packetorframe.InputUnion, to int32) error {
		return fmt.Errorf("denied")
	})

	err := sw.SetValue(ctx, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "onSwitchRequest failed")
	// CurrentValue should not have changed
	assert.Equal(t, int32(0), sw.CurrentValue.Load())
}

func TestOnSwitchRequest_Success(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	ctx := context.Background()

	requestedTo := int32(-1)
	sw.SetOnSwitchRequest(func(ctx context.Context, pkt packetorframe.InputUnion, to int32) error {
		requestedTo = to
		return nil
	})

	err := sw.SetValue(ctx, 5)
	assert.NoError(t, err)
	assert.Equal(t, int32(5), requestedTo)
	assert.Equal(t, int32(5), sw.CurrentValue.Load())
}

func TestOnBeforeSwitch_Called(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	ctx := context.Background()

	var beforeFrom, beforeTo int32
	sw.SetOnBeforeSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {
		beforeFrom = from
		beforeTo = to
	})

	err := sw.SetValue(ctx, 2)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), beforeFrom)
	assert.Equal(t, int32(2), beforeTo)
}

func TestOnInterruptedSwitch_CalledOnSameValue(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(3)
	ctx := context.Background()

	interrupted := false
	sw.SetOnInterruptedSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {
		interrupted = true
	})

	err := sw.SetValue(ctx, 3) // Same value
	assert.NoError(t, err)
	assert.True(t, interrupted)
}

func TestSwitchFlagNextOutputStateBlock(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(0) // currentValue == nextValue
	sw.Flags = types.SwitchFlagNextOutputStateBlock
	ctx := context.Background()

	out1 := sw.Output(1) // Non-current output
	state, _ := out1.GetState(ctx, packetorframe.InputUnion{})
	assert.Equal(t, types.StateBlock, state)
}

func TestSwitchFlagInactiveBlock(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.Flags = types.SwitchFlagInactiveBlock
	ctx := context.Background()

	out1 := sw.Output(1) // Non-current, inactive output
	state, _ := out1.GetState(ctx, packetorframe.InputUnion{})
	assert.Equal(t, types.StateBlock, state)
}

func TestGetChangeChan_Signaling(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	ctx := context.Background()

	ch := sw.GetChangeChan()
	require.NotNil(t, ch)

	// Switch value to trigger rotateChangeChan
	err := sw.SetValue(ctx, 1)
	require.NoError(t, err)

	// Old channel should be closed
	select {
	case <-ch:
		// expected - channel was closed
	default:
		t.Fatal("expected change channel to be closed after switch")
	}

	// New channel should be open
	newCh := sw.GetChangeChan()
	select {
	case <-newCh:
		t.Fatal("expected new change channel to be open")
	default:
		// expected
	}
}

func TestOutput_CreatesCorrectSwitchOutput(t *testing.T) {
	sw := NewSwitch()
	out := sw.Output(5)
	assert.Equal(t, int32(5), out.OutputID)
	assert.Same(t, sw, out.Switch)
}

func TestSetNextValueNow_SameAsCurrentNoPriorPending(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(2)
	sw.SetKeepUnless(packetorframecondition.Static(true))
	ctx := context.Background()

	// Set next to current value with no prior pending - should be a no-op
	err := sw.SetValue(ctx, 2)
	assert.NoError(t, err)
	// NextValue stays as 2 since old was MinInt32 (no prior pending)
	assert.Equal(t, int32(2), sw.NextValue.Load())
}

func TestSetNextValueNow_SameAsCurrentWithPriorPending(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(2)
	sw.SetKeepUnless(packetorframecondition.Static(true))
	ctx := context.Background()

	// First set a different next value
	err := sw.SetValue(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int32(5), sw.NextValue.Load())

	// Now set next back to current value - should clear next
	err = sw.SetValue(ctx, 2)
	assert.NoError(t, err)
	assert.Equal(t, int32(math.MinInt32), sw.NextValue.Load())
}

func TestKeepUnless_ConditionFalse_NoCommit(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.SetKeepUnless(packetorframecondition.Static(false)) // never matches
	ctx := context.Background()

	err := sw.SetValue(ctx, 1)
	require.NoError(t, err)

	// NextValue should be set
	assert.Equal(t, int32(1), sw.NextValue.Load())

	// GetState should NOT trigger commit (keepUnless returns false)
	out1 := sw.Output(1)
	state, _ := out1.GetState(ctx, packetorframe.InputUnion{})
	assert.Equal(t, types.StateDrop, state)

	// CurrentValue should still be 0
	assert.Equal(t, int32(0), sw.CurrentValue.Load())
}

func TestNewSwitch_InitialValues(t *testing.T) {
	sw := NewSwitch()
	assert.Equal(t, int32(0), sw.CurrentValue.Load())
	assert.Equal(t, int32(math.MinInt32), sw.NextValue.Load())
	assert.Equal(t, int32(math.MinInt32), sw.PreviousValue.Load())
	assert.NotNil(t, sw.ChangeSignal)
}

func TestPtr(t *testing.T) {
	v := 42
	p := ptr(v)
	require.NotNil(t, p)
	assert.Equal(t, 42, *p)
}
