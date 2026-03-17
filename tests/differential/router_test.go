package differential

import (
	"fmt"
	"strings"
	"testing"
)

// publishMode mirrors Go's PublishMode / Lean's PublishMode.
type publishMode int

const (
	modeUndefined         publishMode = 0
	modeExclusiveTakeover publishMode = 1
	modeExclusiveFail     publishMode = 2
	modeSharedTakeover    publishMode = 3
	modeSharedFail        publishMode = 4
)

func (m publishMode) isExclusive() bool {
	switch m {
	case modeExclusiveTakeover, modeExclusiveFail:
		return true
	default:
		return false
	}
}

// publisher mirrors the Lean Publisher structure (id + mode = identity).
type publisher struct {
	id   int
	mode publishMode
}

// addResult tags matching Lean's AddResult.
const (
	addOK                   = "ok"
	addErrRouteClosed       = "errRouteClosed"
	addErrAlreadyAPublisher = "errAlreadyAPublisher"
	addErrAlreadyHasPublisher = "errAlreadyHasPublisher"
	addErrUnknownMode       = "errUnknownMode"
)

// removeResult tags matching Lean's RemoveResult.
const (
	removeOK                  = "ok"
	removeErrPublisherNotFound = "errPublisherNotFound"
)

// lifecycleResult tags matching Lean's LifecycleResult.
const (
	lifecycleOK             = "ok"
	lifecycleErrAlreadyOpen   = "errAlreadyOpen"
	lifecycleErrAlreadyClosed = "errAlreadyClosed"
)

// routeState mirrors Lean's RouteState.
type routeState struct {
	isOpen     bool
	publishers []publisher
}

func (s *routeState) containsPub(p publisher) bool {
	for _, pub := range s.publishers {
		if pub == p {
			return true
		}
	}
	return false
}

func (s *routeState) findByID(id int) (publisher, bool) {
	for _, pub := range s.publishers {
		if pub.id == id {
			return pub, true
		}
	}
	return publisher{}, false
}

// addPublisher mirrors Lean's RouteState.addPublisher.
func (s *routeState) addPublisher(p publisher) string {
	if !s.isOpen {
		return addErrRouteClosed
	}
	if s.containsPub(p) {
		return addErrAlreadyAPublisher
	}
	if len(s.publishers) == 0 {
		s.publishers = append(s.publishers, p)
		return addOK
	}

	switch p.mode {
	case modeExclusiveTakeover:
		s.publishers = []publisher{p}
		return addOK
	case modeExclusiveFail:
		return addErrAlreadyHasPublisher
	case modeSharedTakeover:
		kept := make([]publisher, 0, len(s.publishers))
		for _, pub := range s.publishers {
			if !pub.mode.isExclusive() {
				kept = append(kept, pub)
			}
		}
		s.publishers = append(kept, p)
		return addOK
	case modeSharedFail:
		for _, pub := range s.publishers {
			if pub.mode.isExclusive() {
				return addErrAlreadyHasPublisher
			}
		}
		s.publishers = append(s.publishers, p)
		return addOK
	case modeUndefined:
		return addErrUnknownMode
	default:
		return addErrUnknownMode
	}
}

// removePublisher mirrors Lean's RouteState.removePublisher.
// The DiffTest looks up by id (finding the actual publisher in the list),
// then removes by (id, mode) equality.
func (s *routeState) removePublisher(id int) string {
	pub, found := s.findByID(id)
	if !found {
		return removeErrPublisherNotFound
	}
	newPubs := make([]publisher, 0, len(s.publishers))
	for _, p := range s.publishers {
		if p != pub {
			newPubs = append(newPubs, p)
		}
	}
	s.publishers = newPubs
	return removeOK
}

// openNode mirrors Lean's RouteState.openNode.
func (s *routeState) openNode() string {
	if s.isOpen {
		return lifecycleErrAlreadyOpen
	}
	s.isOpen = true
	return lifecycleOK
}

// closeNode mirrors Lean's RouteState.closeNode.
func (s *routeState) closeNode() string {
	if !s.isOpen {
		return lifecycleErrAlreadyClosed
	}
	s.isOpen = false
	return lifecycleOK
}

// processRouterOp processes one line and returns the expected output.
func processRouterOp(s *routeState, line string) string {
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return ""
	}

	switch tokens[0] {
	case "add":
		id := mustAtoi(tokens[1])
		mode := publishMode(mustAtoi(tokens[2]))
		tag := s.addPublisher(publisher{id: id, mode: mode})
		return fmt.Sprintf("%s %d", tag, len(s.publishers))
	case "remove":
		id := mustAtoi(tokens[1])
		tag := s.removePublisher(id)
		return fmt.Sprintf("%s %d", tag, len(s.publishers))
	case "open":
		tag := s.openNode()
		return fmt.Sprintf("%s %d", tag, len(s.publishers))
	case "close":
		tag := s.closeNode()
		return fmt.Sprintf("%s %d", tag, len(s.publishers))
	default:
		panic("unknown op: " + tokens[0])
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// routerOp is one operation in a test sequence.
type routerOp struct {
	line string
}

func opAdd(id int, mode publishMode) routerOp {
	return routerOp{line: fmt.Sprintf("add %d %d", id, mode)}
}

func opRemove(id int) routerOp {
	return routerOp{line: fmt.Sprintf("remove %d", id)}
}

func opOpen() routerOp {
	return routerOp{line: "open"}
}

func opClose() routerOp {
	return routerOp{line: "close"}
}

func TestDiffRouter(t *testing.T) {
	// Generate test sequences covering all mode combinations, conflicts, and lifecycle.
	type testSequence struct {
		name string
		ops  []routerOp
	}

	var sequences []testSequence

	// 1. Single-publisher add for each mode
	for mode := publishMode(0); mode <= 4; mode++ {
		sequences = append(sequences, testSequence{
			name: fmt.Sprintf("single_add_mode%d", mode),
			ops:  []routerOp{opAdd(1, mode)},
		})
	}

	// 2. Add then remove
	for mode := publishMode(0); mode <= 4; mode++ {
		sequences = append(sequences, testSequence{
			name: fmt.Sprintf("add_remove_mode%d", mode),
			ops:  []routerOp{opAdd(1, mode), opRemove(1)},
		})
	}

	// 3. Duplicate add (same id + mode)
	for mode := publishMode(1); mode <= 4; mode++ {
		sequences = append(sequences, testSequence{
			name: fmt.Sprintf("duplicate_add_mode%d", mode),
			ops:  []routerOp{opAdd(1, mode), opAdd(1, mode)},
		})
	}

	// 4. Two publishers: all mode pairs
	for m1 := publishMode(1); m1 <= 4; m1++ {
		for m2 := publishMode(1); m2 <= 4; m2++ {
			sequences = append(sequences, testSequence{
				name: fmt.Sprintf("two_pubs_m%d_m%d", m1, m2),
				ops:  []routerOp{opAdd(1, m1), opAdd(2, m2)},
			})
		}
	}

	// 5. Same id, different modes (publisher identity is id+mode)
	for m1 := publishMode(1); m1 <= 4; m1++ {
		for m2 := publishMode(1); m2 <= 4; m2++ {
			if m1 == m2 {
				continue
			}
			sequences = append(sequences, testSequence{
				name: fmt.Sprintf("same_id_diff_mode_m%d_m%d", m1, m2),
				ops:  []routerOp{opAdd(1, m1), opAdd(1, m2)},
			})
		}
	}

	// 6. Three publishers with removal
	sequences = append(sequences, testSequence{
		name: "three_shared_remove_middle",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeSharedFail),
			opAdd(3, modeSharedTakeover),
			opRemove(2),
		},
	})

	// 7. Exclusive takeover clears all
	sequences = append(sequences, testSequence{
		name: "exclusive_takeover_clears",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeSharedFail),
			opAdd(3, modeExclusiveTakeover),
		},
	})

	// 8. Shared takeover removes exclusives only
	sequences = append(sequences, testSequence{
		name: "shared_takeover_removes_exclusives",
		ops: []routerOp{
			opAdd(1, modeExclusiveTakeover),
			opAdd(2, modeSharedTakeover),
		},
	})

	// 9. Lifecycle: close then add
	sequences = append(sequences, testSequence{
		name: "close_then_add",
		ops:  []routerOp{opClose(), opAdd(1, modeExclusiveTakeover)},
	})

	// 10. Lifecycle: double open, double close
	sequences = append(sequences, testSequence{
		name: "double_open",
		ops:  []routerOp{opOpen()},
	})
	sequences = append(sequences, testSequence{
		name: "close_then_double_close",
		ops:  []routerOp{opClose(), opClose()},
	})

	// 11. Full lifecycle with publishers
	sequences = append(sequences, testSequence{
		name: "lifecycle_with_pubs",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeSharedFail),
			opClose(),
			opAdd(3, modeSharedTakeover),
			opOpen(),
			opAdd(3, modeSharedTakeover),
		},
	})

	// 12. Remove nonexistent publisher
	sequences = append(sequences, testSequence{
		name: "remove_nonexistent",
		ops:  []routerOp{opRemove(99)},
	})

	// 13. Remove after close (publishers persist but route is closed)
	sequences = append(sequences, testSequence{
		name: "remove_after_close",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opClose(),
			opRemove(1),
		},
	})

	// 14. Undefined mode with existing publishers
	sequences = append(sequences, testSequence{
		name: "undefined_mode_with_existing",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeUndefined),
		},
	})

	// 15. Exhaustive mode-conflict matrix: first publisher exists, second tries each mode
	for m1 := publishMode(1); m1 <= 4; m1++ {
		for m2 := publishMode(0); m2 <= 4; m2++ {
			sequences = append(sequences, testSequence{
				name: fmt.Sprintf("conflict_first%d_second%d", m1, m2),
				ops: []routerOp{
					opAdd(1, m1),
					opRemove(1),
					opAdd(2, m1),
					opAdd(3, m2),
				},
			})
		}
	}

	// 16. Long sequence: add many shared, then exclusive takeover
	{
		var ops []routerOp
		for i := 1; i <= 5; i++ {
			ops = append(ops, opAdd(i, modeSharedTakeover))
		}
		ops = append(ops, opAdd(100, modeExclusiveTakeover))
		sequences = append(sequences, testSequence{
			name: "many_shared_then_exclusive_takeover",
			ops:  ops,
		})
	}

	// 17. Alternating add/remove
	{
		var ops []routerOp
		for i := 1; i <= 5; i++ {
			ops = append(ops, opAdd(i, modeSharedTakeover))
			ops = append(ops, opRemove(i))
		}
		sequences = append(sequences, testSequence{
			name: "alternating_add_remove",
			ops:  ops,
		})
	}

	// 18. SharedFail blocked by exclusive
	sequences = append(sequences, testSequence{
		name: "shared_fail_blocked_by_exclusive",
		ops: []routerOp{
			opAdd(1, modeExclusiveTakeover),
			opAdd(2, modeSharedFail),
		},
	})

	// 19. SharedFail succeeds with only shared present
	sequences = append(sequences, testSequence{
		name: "shared_fail_succeeds_with_shared",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeSharedFail),
		},
	})

	// 20. ExclusiveFail with existing publisher
	sequences = append(sequences, testSequence{
		name: "exclusive_fail_blocked",
		ops: []routerOp{
			opAdd(1, modeSharedTakeover),
			opAdd(2, modeExclusiveFail),
		},
	})

	// Each sequence runs in a fresh Lean process (state resets to open, empty publishers).
	totalVectors := 0
	mismatches := 0

	for _, seq := range sequences {
		// Build input and Go-side expected output for this sequence.
		var seqInput strings.Builder
		var seqExpected []string
		state := routeState{isOpen: true, publishers: nil}
		for _, op := range seq.ops {
			fmt.Fprintln(&seqInput, op.line)
			seqExpected = append(seqExpected, processRouterOp(&state, op.line))
		}
		totalVectors += len(seq.ops)

		leanOutput := runDifftest(t, "router", seqInput.String())
		leanLines := strings.Split(leanOutput, "\n")

		if len(leanLines) != len(seqExpected) {
			t.Fatalf("seq=%s: expected %d output lines, got %d",
				seq.name, len(seqExpected), len(leanLines))
		}

		for i, op := range seq.ops {
			if seqExpected[i] != leanLines[i] {
				mismatches++
				t.Errorf("MISMATCH seq=%s op=%q: Go=%q Lean=%q",
					seq.name, op.line, seqExpected[i], leanLines[i])
				if mismatches >= 20 {
					t.Fatalf("too many mismatches (%d), stopping early", mismatches)
				}
			}
		}
	}

	t.Logf("Tested %d vectors across %d sequences, %d mismatches",
		totalVectors, len(sequences), mismatches)
}
