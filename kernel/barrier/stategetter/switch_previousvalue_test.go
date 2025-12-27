package stategetter_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// waitForPass simulates the barrier's blocking behavior:
// loops until StatePass or context cancelled
func waitForPass(ctx context.Context, sg stategetter.StateGetter, pkt packetorframe.InputUnion) types.State {
	for {
		state, changeCh := sg.GetState(ctx, pkt)
		switch state {
		case types.StatePass:
			return state
		case types.StateBlock:
			select {
			case <-changeCh:
				// state changed, retry
			case <-ctx.Done():
				return state // return last state on context cancel
			}
		case types.StateDrop:
			return state
		}
	}
}

// TestSwitchFlagPassPreviousOutput tests that SwitchFlagPassPreviousOutput prevents deadlock
// when packets for the previous output are still in transit after a switch.
//
// Scenario being tested:
// 1. Switch is set to output 1
// 2. Packet passes through Switch (for output 1)
// 3. Switch changes to output 2
// 4. Packet reaches Syncer (still expecting output 1)
// 5. WITHOUT the fix: Syncer blocks forever (only accepts output 2)
// 6. WITH the fix: Syncer passes packet for previousValue (output 1)
func TestSwitchFlagPassPreviousOutput(t *testing.T) {
	ctx := context.Background()

	outputSwitch := stategetter.NewSwitch()
	outputSyncer := stategetter.NewSwitch()

	// Configure syncer with SwitchFlagInactiveBlock and SwitchFlagPassPreviousOutput (the fix)
	outputSyncer.Flags.Set(types.SwitchFlagInactiveBlock | types.SwitchFlagPassPreviousOutput)

	// Set up OnAfterSwitch to update syncer after switch changes
	outputSwitch.SetOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32) {
		outputSyncer.SetValue(ctx, to)
	})

	// Initial state: both set to output 1
	require.NoError(t, outputSwitch.SetValue(ctx, 1))
	require.NoError(t, outputSyncer.SetValue(ctx, 1))

	pkt := packetorframe.InputUnion{}

	// 1. Verify packet passes switch for output 1
	switchOutput := outputSwitch.Output(1)
	state, _ := switchOutput.GetState(ctx, pkt)
	require.Equal(t, types.StatePass, state, "packet should pass switch for output 1")

	// 2. Switch changes to output 2 (triggers OnAfterSwitch which updates syncer)
	require.NoError(t, outputSwitch.SetValue(ctx, 2))

	// 3. Packet reaches syncer - should pass because of SwitchFlagPassPreviousOutput
	syncerOutput1 := outputSyncer.Output(1)

	resultCh := make(chan types.State, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		state := waitForPass(ctx, syncerOutput1, pkt)
		resultCh <- state
	}()

	select {
	case state := <-resultCh:
		require.Equal(t, types.StatePass, state, "packet for previousValue should pass with SwitchFlagPassPreviousOutput")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("DEADLOCK: syncer blocked packet for previousValue - SwitchFlagPassPreviousOutput is not working")
	}

	wg.Wait()

	// 4. Clear previous value (simulates closing old output)
	outputSyncer.ClearPreviousValue(ctx)

	// 5. Now packets for output 1 should block
	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	resultCh2 := make(chan types.State, 1)
	go func() {
		state := waitForPass(ctx2, syncerOutput1, pkt)
		resultCh2 <- state
	}()

	select {
	case state := <-resultCh2:
		require.Equal(t, types.StateBlock, state, "after ClearPreviousValue, packets for old output should block")
	case <-time.After(150 * time.Millisecond):
		// Also acceptable - it blocked
	}
}

// TestSwitchFlagPassPreviousOutput_WithoutFix reproduces the deadlock bug
func TestSwitchFlagPassPreviousOutput_WithoutFix(t *testing.T) {
	ctx := context.Background()

	outputSwitch := stategetter.NewSwitch()
	outputSyncer := stategetter.NewSwitch()

	// Only SwitchFlagInactiveBlock - NO SwitchFlagPassPreviousOutput (reproduces bug)
	outputSyncer.Flags.Set(types.SwitchFlagInactiveBlock)

	outputSwitch.SetOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32) {
		outputSyncer.SetValue(ctx, to)
	})

	require.NoError(t, outputSwitch.SetValue(ctx, 1))
	require.NoError(t, outputSyncer.SetValue(ctx, 1))

	pkt := packetorframe.InputUnion{}

	// Switch changes to 2
	require.NoError(t, outputSwitch.SetValue(ctx, 2))

	syncerOutput1 := outputSyncer.Output(1)

	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	resultCh := make(chan types.State, 1)
	go func() {
		state := waitForPass(ctx2, syncerOutput1, pkt)
		resultCh <- state
	}()

	// Should timeout/block because without the fix, packets for old output block forever
	select {
	case state := <-resultCh:
		require.Equal(t, types.StateBlock, state, "without fix, packets for old output should block")
	case <-time.After(150 * time.Millisecond):
		// Expected - it blocked until timeout
	}
}
