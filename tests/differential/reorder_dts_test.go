package differential

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reorderItem mirrors Lean's Item structure.
type reorderItem struct {
	dts       int64
	streamKey uint
}

// reorderState mirrors Lean's ReorderState.
type reorderState struct {
	globalQueue      []reorderItem
	streamQueues     map[uint][]int64
	knownStreams     []uint
	emptyQueuesCount uint
	prevDTS          int64
	maxDTSDiff       uint
	capacity         uint
	discardMode      bool
	emitted          []int64 // most recent first
}

func newReorderState(maxDiff, cap uint, discard bool) *reorderState {
	return &reorderState{
		streamQueues: make(map[uint][]int64),
		prevDTS:      -1,
		maxDTSDiff:   maxDiff,
		capacity:     cap,
		discardMode:  discard,
	}
}

// currentDTS returns the minimum DTS in the global queue, or (0, false) if empty.
func (s *reorderState) currentDTS() (int64, bool) {
	if len(s.globalQueue) == 0 {
		return 0, false
	}
	return s.globalQueue[0].dts, true
}

// sortedInsertItem inserts item into the global queue maintaining ascending DTS order.
func sortedInsertItem(item reorderItem, queue []reorderItem) []reorderItem {
	// Find insertion point
	for i, h := range queue {
		if item.dts <= h.dts {
			result := make([]reorderItem, len(queue)+1)
			copy(result, queue[:i])
			result[i] = item
			copy(result[i+1:], queue[i:])
			return result
		}
	}
	return append(queue, item)
}

// sortedInsertDTS inserts a DTS value into a sorted ascending list.
func sortedInsertDTS(x int64, queue []int64) []int64 {
	for i, h := range queue {
		if x <= h {
			result := make([]int64, len(queue)+1)
			copy(result, queue[:i])
			result[i] = x
			copy(result[i+1:], queue[i:])
			return result
		}
	}
	return append(queue, x)
}

type sendResult int

const (
	sendResultSent sendResult = iota
	sendResultDiscarded
	sendResultErrBackward
)

// doSendItem attempts to emit an item, checking DTS monotonicity.
func (s *reorderState) doSendItem(dts int64) sendResult {
	if s.prevDTS > dts {
		if s.discardMode {
			return sendResultDiscarded
		}
		return sendResultErrBackward
	}
	s.prevDTS = dts
	s.emitted = append([]int64{dts}, s.emitted...)
	return sendResultSent
}

// sendOneItemFromQueue pops the global minimum, updates stream queue,
// updates emptyQueuesCount, calls doSendItem.
// Returns false if queue was empty.
func (s *reorderState) sendOneItemFromQueue() bool {
	if len(s.globalQueue) == 0 {
		return false
	}

	item := s.globalQueue[0]
	s.globalQueue = s.globalQueue[1:]

	streamQ := s.streamQueues[item.streamKey]
	var newStreamQ []int64
	if len(streamQ) > 0 {
		newStreamQ = streamQ[1:]
	}

	nowEmpty := len(newStreamQ) == 0
	if nowEmpty && len(streamQ) > 0 {
		s.emptyQueuesCount++
	}

	s.streamQueues[item.streamKey] = newStreamQ
	s.doSendItem(item.dts)
	return true
}

// flushUntilClose flushes old items until currentDTS + maxDiff >= targetDTS or queue empty.
func (s *reorderState) flushUntilClose(targetDTS int64, fuel uint) {
	if fuel == 0 {
		return
	}
	curDTS, ok := s.currentDTS()
	if !ok {
		return
	}
	if curDTS+int64(s.maxDTSDiff) >= targetDTS {
		return
	}
	if !s.sendOneItemFromQueue() {
		return
	}
	s.flushUntilClose(targetDTS, fuel-1)
}

// enforceLowDTSDifference checks and enforces DTS difference constraints.
// Returns true if the item should be accepted, false if it should be discarded.
func (s *reorderState) enforceLowDTSDifference(newDTS int64) bool {
	curDTS, ok := s.currentDTS()
	if !ok {
		return true
	}

	diff := newDTS - curDTS
	if -diff > int64(s.maxDTSDiff) {
		return false
	}
	if diff > int64(s.maxDTSDiff) {
		s.flushUntilClose(newDTS, uint(len(s.globalQueue)))
	}
	return true
}

// pullAndSendPendingItems sends items while all known streams have data.
func (s *reorderState) pullAndSendPendingItems(fuel uint) {
	if fuel == 0 {
		return
	}
	if s.emptyQueuesCount != 0 {
		return
	}
	if !s.sendOneItemFromQueue() {
		return
	}
	s.pullAndSendPendingItems(fuel - 1)
}

// uintSliceContainsKey checks if a stream key is in the known streams list.
func uintSliceContainsKey(s []uint, v uint) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// pushToQueue is the main entry point for adding an item.
func (s *reorderState) pushToQueue(item reorderItem) {
	// Step 1: enforce low DTS difference
	if !s.enforceLowDTSDifference(item.dts) {
		return
	}

	// Step 2: handle full queue
	if uint(len(s.globalQueue)) >= s.capacity {
		if s.discardMode {
			if len(s.globalQueue) > 0 {
				oldest := s.globalQueue[0]
				s.globalQueue = s.globalQueue[1:]
				sq := s.streamQueues[oldest.streamKey]
				var newSq []int64
				if len(sq) > 0 {
					newSq = sq[1:]
				}
				if len(newSq) == 0 && len(sq) > 0 {
					s.emptyQueuesCount++
				}
				s.streamQueues[oldest.streamKey] = newSq
			}
		} else {
			s.sendOneItemFromQueue()
		}
	}

	// Step 3: push item to both queues
	s.globalQueue = sortedInsertItem(item, s.globalQueue)
	streamQ := s.streamQueues[item.streamKey]
	newStreamQ := sortedInsertDTS(item.dts, streamQ)
	s.streamQueues[item.streamKey] = newStreamQ

	isNewStream := !uintSliceContainsKey(s.knownStreams, item.streamKey)
	if isNewStream {
		s.knownStreams = append([]uint{item.streamKey}, s.knownStreams...)
		// emptyQueuesCount unchanged for new stream (Lean: eqcNew = s₂.emptyQueuesCount)
	} else if len(streamQ) == 0 {
		// Stream queue was empty, now has an item: decrement empty count
		if s.emptyQueuesCount > 0 {
			s.emptyQueuesCount--
		}
	}

	// Step 4: if all queues have data, pull and send
	if s.emptyQueuesCount == 0 {
		s.pullAndSendPendingItems(uint(len(s.globalQueue)))
	}
}

// flushAll drains all remaining items from the global queue.
func (s *reorderState) flushAll() {
	fuel := uint(len(s.globalQueue))
	for i := uint(0); i < fuel; i++ {
		if !s.sendOneItemFromQueue() {
			return
		}
	}
}

// runReorderGo runs the Go reimplementation of the reorder algorithm.
// Returns per-push emission counts and DTS lists, plus the final flush list.
func runReorderGo(
	maxDiff, cap uint,
	discard bool,
	items []reorderItem,
) (perPush [][]int64, flushDTS []int64) {
	s := newReorderState(maxDiff, cap, discard)

	for _, item := range items {
		emittedBefore := len(s.emitted)
		s.pushToQueue(item)

		newCount := len(s.emitted) - emittedBefore
		var newEmitted []int64
		if newCount > 0 {
			// emitted is most-recent-first; new items are the first newCount entries
			newEmittedReversed := s.emitted[:newCount]
			// Reverse for chronological order
			newEmitted = make([]int64, newCount)
			for i, v := range newEmittedReversed {
				newEmitted[newCount-1-i] = v
			}
		}
		perPush = append(perPush, newEmitted)
	}

	emittedBefore := len(s.emitted)
	s.flushAll()
	newCount := len(s.emitted) - emittedBefore
	if newCount > 0 {
		newEmittedReversed := s.emitted[:newCount]
		flushDTS = make([]int64, newCount)
		for i, v := range newEmittedReversed {
			flushDTS[newCount-1-i] = v
		}
	}

	return perPush, flushDTS
}

// formatReorderInput builds the difftest stdin for the reorderdts component.
func formatReorderInput(maxDiff, cap uint, discard bool, items []reorderItem) string {
	var sb strings.Builder
	discardInt := 0
	if discard {
		discardInt = 1
	}

	// Count unique streams
	streamSet := make(map[uint]struct{})
	for _, item := range items {
		streamSet[item.streamKey] = struct{}{}
	}

	fmt.Fprintf(&sb, "%d %d %d %d\n", maxDiff, cap, discardInt, len(streamSet))
	for _, item := range items {
		fmt.Fprintf(&sb, "%d %d\n", item.dts, item.streamKey)
	}
	return sb.String()
}

// formatGoReorderOutput formats the Go output to match Lean's line-based protocol.
func formatGoReorderOutput(perPush [][]int64, flushDTS []int64) string {
	var lines []string
	for _, emitted := range perPush {
		if len(emitted) == 0 {
			lines = append(lines, "0")
		} else {
			parts := []string{fmt.Sprintf("%d", len(emitted))}
			for _, dts := range emitted {
				parts = append(parts, fmt.Sprintf("%d", dts))
			}
			lines = append(lines, strings.Join(parts, " "))
		}
	}

	if len(flushDTS) == 0 {
		lines = append(lines, "flush")
	} else {
		parts := []string{"flush"}
		for _, dts := range flushDTS {
			parts = append(parts, fmt.Sprintf("%d", dts))
		}
		lines = append(lines, strings.Join(parts, " "))
	}

	return strings.Join(lines, "\n")
}

func TestDiffReorderDTS(t *testing.T) {
	type testCase struct {
		name    string
		maxDiff uint
		cap     uint
		discard bool
		items   []reorderItem
	}

	cases := []testCase{
		{
			name:    "single stream, already sorted",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 0},
				{dts: 3, streamKey: 0},
			},
		},
		{
			name:    "single stream, reverse order",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 3, streamKey: 0},
				{dts: 2, streamKey: 0},
				{dts: 1, streamKey: 0},
			},
		},
		{
			name:    "two streams, interleaved monotonic",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 1},
				{dts: 3, streamKey: 0},
				{dts: 4, streamKey: 1},
			},
		},
		{
			name:    "two streams, out of order across streams",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 5, streamKey: 0},
				{dts: 3, streamKey: 1},
				{dts: 7, streamKey: 0},
				{dts: 6, streamKey: 1},
			},
		},
		{
			name:    "three streams, interleaved",
			maxDiff: 20,
			cap:     10,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 1},
				{dts: 3, streamKey: 2},
				{dts: 4, streamKey: 0},
				{dts: 5, streamKey: 1},
				{dts: 6, streamKey: 2},
			},
		},
		{
			name:    "exceed MaxDTSDifference, flush triggered",
			maxDiff: 5,
			cap:     10,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 1},
				{dts: 20, streamKey: 0},
			},
		},
		{
			name:    "item too far behind, discard mode",
			maxDiff: 5,
			cap:     10,
			discard: true,
			items: []reorderItem{
				{dts: 10, streamKey: 0},
				{dts: 11, streamKey: 1},
				{dts: 2, streamKey: 0},
			},
		},
		{
			name:    "item too far behind, non-discard mode",
			maxDiff: 5,
			cap:     10,
			discard: false,
			items: []reorderItem{
				{dts: 10, streamKey: 0},
				{dts: 11, streamKey: 1},
				{dts: 2, streamKey: 0},
			},
		},
		{
			name:    "queue overflow, discard mode",
			maxDiff: 100,
			cap:     3,
			discard: true,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 0},
				{dts: 3, streamKey: 0},
				{dts: 4, streamKey: 0},
			},
		},
		{
			name:    "queue overflow, non-discard mode",
			maxDiff: 100,
			cap:     3,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 0},
				{dts: 3, streamKey: 0},
				{dts: 4, streamKey: 0},
			},
		},
		{
			name:    "two streams, queue overflow forces emission",
			maxDiff: 100,
			cap:     2,
			discard: false,
			items: []reorderItem{
				{dts: 5, streamKey: 0},
				{dts: 3, streamKey: 1},
				{dts: 7, streamKey: 0},
			},
		},
		{
			name:    "equal DTS values across streams",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 5, streamKey: 0},
				{dts: 5, streamKey: 1},
				{dts: 5, streamKey: 0},
			},
		},
		{
			name:    "empty input",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items:   nil,
		},
		{
			name:    "single item",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 42, streamKey: 0},
			},
		},
		{
			name:    "large DTS gap within tolerance",
			maxDiff: 1000,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 100, streamKey: 0},
				{dts: 200, streamKey: 1},
				{dts: 300, streamKey: 0},
			},
		},
		{
			name:    "capacity 1, single stream",
			maxDiff: 100,
			cap:     1,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 0},
				{dts: 3, streamKey: 0},
			},
		},
		{
			name:    "two streams, second stream arrives late",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 3, streamKey: 0},
				{dts: 5, streamKey: 0},
				{dts: 2, streamKey: 1},
			},
		},
		{
			name:    "zero DTS values",
			maxDiff: 10,
			cap:     5,
			discard: false,
			items: []reorderItem{
				{dts: 0, streamKey: 0},
				{dts: 0, streamKey: 1},
			},
		},
		{
			name:    "discard mode, capacity overflow with two streams",
			maxDiff: 100,
			cap:     2,
			discard: true,
			items: []reorderItem{
				{dts: 1, streamKey: 0},
				{dts: 2, streamKey: 1},
				{dts: 3, streamKey: 0},
			},
		},
	}

	mismatches := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perPush, flushDTS := runReorderGo(tc.maxDiff, tc.cap, tc.discard, tc.items)
			goOutput := formatGoReorderOutput(perPush, flushDTS)

			input := formatReorderInput(tc.maxDiff, tc.cap, tc.discard, tc.items)
			leanOutput := runDifftest(t, "reorderdts", input)

			if !assert.Equal(t, goOutput, leanOutput,
				"Go vs Lean mismatch for %s\ninput:\n%s", tc.name, input) {
				mismatches++
			}
		})
	}

	require.Zero(t, mismatches, "%d mismatches found", mismatches)
	t.Logf("All %d reorder DTS test vectors matched between Go and Lean", len(cases))
}
