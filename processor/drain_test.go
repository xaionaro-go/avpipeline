package processor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
)

// bufferedProcessor is a minimal Abstract implementation with a controllable
// buffered input channel for testing drain functions. It does NOT start any
// background goroutines, unlike FromKernel.
type bufferedProcessor struct {
	inputCh  chan packetorframe.InputUnion
	counters *Counters
}

func newBufferedProcessor(bufSize int) *bufferedProcessor {
	return &bufferedProcessor{
		inputCh:  make(chan packetorframe.InputUnion, bufSize),
		counters: processortypes.NewCounters(),
	}
}

func (b *bufferedProcessor) String() string                               { return "bufferedProcessor" }
func (b *bufferedProcessor) Close(context.Context) error                  { return nil }
func (b *bufferedProcessor) InputChan() chan<- packetorframe.InputUnion   { return b.inputCh }
func (b *bufferedProcessor) OutputChan() <-chan packetorframe.OutputUnion { return nil }
func (b *bufferedProcessor) ErrorChan() <-chan error                      { return nil }
func (b *bufferedProcessor) CountersPtr() *Counters                       { return b.counters }

func TestIsInputDrained_EmptyQueue(t *testing.T) {
	d := NewDummy()
	// Dummy's InputChan returns DiscardInputChan which is an unbuffered channel.
	// len() of an unbuffered channel is always 0 -> drained.
	assert.True(t, IsInputDrained(d), "empty queue should be considered drained")
}

func TestIsInputDrained_NonEmptyQueue(t *testing.T) {
	bp := newBufferedProcessor(10)
	// Put something in the input channel
	bp.inputCh <- packetorframe.InputUnion{}
	assert.False(t, IsInputDrained(bp), "non-empty queue should not be considered drained")
}

func TestIsInputDrained_BufferedButEmpty(t *testing.T) {
	bp := newBufferedProcessor(10)
	assert.True(t, IsInputDrained(bp), "buffered but empty queue should be considered drained")
}

func TestDrainInput_EmptyProcessor(t *testing.T) {
	d := NewDummy()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := DrainInput(ctx, d)
	assert.NoError(t, err, "draining an empty processor should succeed immediately")
}

func TestDrainInput_ContextCancellation(t *testing.T) {
	bp := newBufferedProcessor(100)

	// Fill up the input channel with enough items
	for i := 0; i < 50; i++ {
		bp.inputCh <- packetorframe.InputUnion{}
	}

	// Use a very short timeout so the drain will fail
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer drainCancel()

	err := DrainInput(drainCtx, bp)
	// Should return context error because no one is consuming from the queue
	assert.Error(t, err, "drain should fail when context expires before queue drains")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDrainInput_DrainsOverTime(t *testing.T) {
	bp := newBufferedProcessor(10)
	bp.inputCh <- packetorframe.InputUnion{}

	// Start a goroutine that consumes from the channel after a short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		<-bp.inputCh
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := DrainInput(ctx, bp)
	assert.NoError(t, err, "drain should succeed once the queue becomes empty")
}
