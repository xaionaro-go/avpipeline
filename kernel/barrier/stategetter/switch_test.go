package stategetter

import (
	"context"
	"math"
	"testing"

	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

func TestSwitchInitialStateAndChannels(t *testing.T) {
	sw := NewSwitch()
	out0 := sw.Output(0)
	// initial current value is 0 (atomic.Int32 default)
	state, ch := out0.GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StatePass {
		t.Fatalf("expected pass for current output, got %v", state)
	}
	if ch == nil {
		t.Fatalf("expected non-nil change channel")
	}

	// other output should drop (no next value)
	out1 := sw.Output(1)
	state1, _ := out1.GetState(context.Background(), packetorframe.InputUnion{})
	if state1 != types.StateDrop {
		t.Fatalf("expected StateDrop for non-current output without next, got %v", state1)
	}
}

func TestSetValueImmediateSwitchWhenNoKeepUnless(t *testing.T) {
	sw := NewSwitch()
	// ensure starting from 0
	sw.CurrentValue.Store(0)
	if err := sw.SetValue(context.Background(), 2); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	if got := sw.CurrentValue.Load(); got != 2 {
		t.Fatalf("expected current value to be set to 2 immediately, got %d", got)
	}
	// NextValue should be set to 2 but cleared immediately by SetValue if no KeepUnless
	if got := sw.NextValue.Load(); got != math.MinInt32 {
		t.Fatalf("expected NextValue to be reset (MinInt32), got %d", got)
	}
}

func TestSetValueWithKeepUnlessAndCommitTriggersOnGetState(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	// set keep-unless so SetValue does not immediately change current value
	sw.SetKeepUnless(packetorframecondition.Static(true))
	// set an onAfter switch hook to observe commit
	called := false
	sw.SetOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32) {
		called = true
		if from != 0 || to != 3 {
			t.Fatalf("unexpected onAfterSwitch args: from=%d to=%d", from, to)
		}
	})
	if err := sw.SetValue(context.Background(), 3); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	// because KeepUnless is set, CurrentValue should still be 0
	if cur := sw.CurrentValue.Load(); cur != 0 {
		t.Fatalf("expected CurrentValue to remain 0 before commit, got %d", cur)
	}
	// calling GetState on output 3 should trigger commit (keepUnless matches true)
	out3 := sw.Output(3)
	state, _ := out3.GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StatePass {
		t.Fatalf("expected pass for newly committed current output, got %v", state)
	}
	// after commit CurrentValue must be updated and PreviousValue set
	if cur := sw.CurrentValue.Load(); cur != 3 {
		t.Fatalf("expected CurrentValue to be 3 after commit, got %d", cur)
	}
	if prev := sw.PreviousValue.Load(); prev != math.MinInt32 {
		t.Fatalf("expected PreviousValue to be math.MinInt32 after commit, got %d", prev)
	}
	if !called {
		t.Fatalf("expected OnAfterSwitch to be called on commit")
	}
	// NextValue should be cleared after commit
	if n := sw.NextValue.Load(); n != math.MinInt32 {
		t.Fatalf("expected NextValue to be cleared after commit, got %d", n)
	}
}

func TestFlagSwitchFirstPacketAfterSwitchPassBothOutputs(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	// set keep-unless so SetValue does not immediately change current value
	sw.SetKeepUnless(packetorframecondition.Static(true))
	if err := sw.SetValue(context.Background(), 3); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	// enable first-packet-pass-both: the first packet after commit should pass previous output too
	sw.Flags = types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs

	// calling GetState on output 3 should trigger commit
	out3 := sw.Output(3)
	state3, _ := out3.GetState(context.Background(), packetorframe.InputUnion{})
	if state3 != types.StatePass {
		t.Fatalf("expected block for newly committed current output, got %v", state3)
	}
	// after commit CurrentValue must be updated and PreviousValue set
	if cur := sw.CurrentValue.Load(); cur != 3 {
		t.Fatalf("expected CurrentValue to be 3 after commit, got %d", cur)
	}
	if prev := sw.PreviousValue.Load(); prev != 0 {
		t.Fatalf("expected PreviousValue to be 0 after commit, got %d", prev)
	}
	// previous output (0) should pass for the first packet after switch
	out0 := sw.Output(0)
	state0, _ := out0.GetState(context.Background(), packetorframe.InputUnion{})
	if state0 != types.StatePass {
		t.Fatalf("expected pass for previous output on first packet after switch, got %v", state0)
	}
	// subsequent packets for previous output should not pass
	state0b, _ := out0.GetState(context.Background(), packetorframe.InputUnion{})
	if state0b != types.StateDrop {
		t.Fatalf("expected drop for previous output after first packet, got %v", state0b)
	}
	// NextValue should be cleared after commit
	if n := sw.NextValue.Load(); n != math.MinInt32 {
		t.Fatalf("expected NextValue to be cleared after commit, got %d", n)
	}
}

func TestFlagSwitchForbidTakeoverInKeepUnless(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	// set keep-unless so SetValue does not immediately change current value
	sw.SetKeepUnless(packetorframecondition.Static(true))
	if err := sw.SetValue(context.Background(), 2); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	// enable forbid-takeover: outputs other than current must not commit the next value
	sw.Flags = types.SwitchFlagForbidTakeoverInKeepUnless

	out2 := sw.Output(2)
	state, _ := out2.GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StateDrop {
		t.Fatalf("expected drop for non-current output when forbid-takeover is set, got %v", state)
	}
	// NextValue should remain unchanged (still pending)
	if n := sw.NextValue.Load(); n != 2 {
		t.Fatalf("expected NextValue to remain 2 when takeover is forbidden, got %d", n)
	}

	state, _ = sw.Output(0).GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StateDrop {
		t.Fatalf("expected drop for current output when forbid-takeover is set, got %v", state)
	}

	state, _ = out2.GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StatePass {
		t.Fatalf("expected pass for non-current output when forbid-takeover is set, got %v", state)
	}
}

// Additional tests

func TestSetValueNoSwitchOnSameValue(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(1)
	called := false
	sw.SetOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32) {
		called = true
	})
	// setting to same value should be a no-op and should not call onAfter
	if err := sw.SetValue(context.Background(), 1); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	if called {
		t.Fatalf("expected OnAfterSwitch not to be called when setting same current value")
	}
	// NextValue should remain cleared
	if n := sw.NextValue.Load(); n != math.MinInt32 {
		t.Fatalf("expected NextValue to remain MinInt32, got %d", n)
	}
}

func TestSetValueKeepUnlessMultiplePending(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.SetKeepUnless(packetorframecondition.Static(true))
	if err := sw.SetValue(context.Background(), 2); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	// overwrite pending next with another request
	if err := sw.SetValue(context.Background(), 3); err != nil {
		t.Fatalf("SetValue returned error: %v", err)
	}
	if n := sw.NextValue.Load(); n != 3 {
		t.Fatalf("expected NextValue to be overwritten to 3, got %d", n)
	}
	// committing via GetState on output 3
	out3 := sw.Output(3)
	state, _ := out3.GetState(context.Background(), packetorframe.InputUnion{})
	if state != types.StatePass {
		t.Fatalf("expected pass after commit to 3, got %v", state)
	}
	if cur := sw.CurrentValue.Load(); cur != 3 {
		t.Fatalf("expected CurrentValue to be 3 after commit, got %d", cur)
	}
}
