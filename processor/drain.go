package processor

import (
	"context"
	"sync/atomic"
	"time"
)

type Flusher interface {
	IsDirty(
		ctx context.Context,
	) bool
	Flush(
		ctx context.Context,
	) error
}

type ProcessingState struct {
	PendingPackets   int
	PendingFrames    int
	IsProcessorDirty bool
	IsProcessing     bool
	InputSent        atomic.Bool
}

func IsInputDrained(p Abstract) bool {
	return len(p.InputPacketChan()) == 0 && len(p.InputFrameChan()) == 0
}

func DrainInput(
	ctx context.Context,
	p Abstract,
) error {
	// TODO: find a better solution:
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		if IsInputDrained(p) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	return nil
}
