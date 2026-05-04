// stream_mux_node.go implements the node interface for the stream muxer.

package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*StreamMux[struct{}])(nil)

func (s *StreamMux[C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Tracef(ctx, "StreamMux.Serve(ctx, %#+v, %p)", cfg, errCh)
	defer logger.Tracef(ctx, "/StreamMux.Serve(ctx, %#+v, %p)", cfg, errCh)
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	startCh := *xatomic.LoadPointer(&s.startedCh)
	select {
	case <-startCh:
		panic("this StreamMux is already serving")
	default:
	}
	close(startCh)
	defer func() {
		xatomic.StorePointer(&s.startedCh, ptr(make(chan struct{})))
	}()
	observability.Go(ctx, func(ctx context.Context) {
		s.inputBitRateMeasurerLoop(ctx)
	})
	observability.Go(ctx, func(ctx context.Context) {
		s.latencyMeasurerLoop(ctx)
	})
	// 1 Hz retry tick for the no-sibling eviction-recovery state.
	// Without this, an eviction whose first recreate fails leaves the
	// input demoted to MinInt32 with no further trigger to re-attempt
	// — the SwitchOutput.GetState defense-in-depth path drops in-flight
	// frames silently, producing no node-level error. The 1 Hz tick is
	// the ONLY path that can heal the orphaned input once the
	// destination becomes reachable again. See RETRY_SEMANTICS.md.
	s.startEvictionRecreateRetryLoop(ctx)
	rawErrCh := make(chan node.Error, 100)
	defer close(rawErrCh)
	observability.Go(ctx, func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case nodeErr, ok := <-rawErrCh:
				if !ok {
					return
				}
				logger.Tracef(ctx, "got error from rawErrCh: %v", nodeErr)
				if customDataer, ok := nodeErr.Node.(node.GetCustomDataer[OutputCustomData[C]]); ok {
					output := customDataer.GetCustomData().Output
					assert(ctx, output != nil, fmt.Sprintf("<%s> <%T> <%#+v>", nodeErr.Node, nodeErr.Node, nodeErr.Node))
					if err := s.handleOutputNodeError(ctx, output, nodeErr); err == nil {
						// the error was handled
						continue
					}
				} else {
					err := nodeErr.Err
					if h, ok := nodeErr.Node.GetProcessor().(globaltypes.ErrorHandler); ok {
						err = h.HandleError(ctx, nodeErr.Err)
					}
					if err == nil {
						logger.Debugf(ctx, "error from node %T:%s was handled", nodeErr.Node, nodeErr.Node)
						// the error was handled
						continue
					}
					logger.Errorf(ctx, "node %T:%s does not implement GetCustomDataer[OutputCustomData]; do not know how to handle error %v (%v)", nodeErr.Node, nodeErr.Node, nodeErr.Err, err)
				}
				logger.Debugf(ctx, "forwarding error from node %T:%s to errCh: %v", nodeErr.Node, nodeErr.Node, nodeErr.Err)
				select {
				case errCh <- nodeErr:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	avpipeline.Serve(ctx, avpipeline.ServeConfig{
		EachNode:             cfg,
		AutoServeNewBranches: true,
	}, rawErrCh, s.InputAll.Node)
}

func (s *StreamMux[C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(s)
}

func (s *StreamMux[C]) String() string {
	return "StreamMux"
}

func (s *StreamMux[C]) IsServing(ctx context.Context) bool {
	return s.InputAll.Node.IsServing(ctx)
}

func (n *StreamMux[C]) OriginalNodeAbstract() node.Abstract {
	origN := n.OriginalNode()
	if origN == nil {
		return nil
	}
	return origN
}

func (n *StreamMux[C]) OriginalNode() *NodeInput[C] {
	return n.InputAll.Node
}

func (s *StreamMux[C]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return nil
}

func (a *StreamMux[C]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
}

func (s *StreamMux[C]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
}

func (s *StreamMux[C]) SetPushTos(
	ctx context.Context,
	v node.PushTos,
) {
}

func (s *StreamMux[C]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return nil
}

func (s *StreamMux[C]) GetProcessor() processor.Abstract {
	return s
}

func (s *StreamMux[C]) GetChangeChanIsServing() <-chan struct{} {
	return s.InputAll.Node.GetChangeChanIsServing()
}

func (s *StreamMux[C]) GetChangeChanPushTo() <-chan struct{} {
	return nil
}

func (s *StreamMux[C]) GetChangeChanDrained() <-chan struct{} {
	ctx := context.Background()
	return node.CombineGetChangeChanDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, s.Nodes(ctx)...)
}

func (s *StreamMux[C]) Nodes(ctx context.Context) []node.Abstract {
	var nodes []node.Abstract
	if s.InputAll.Node != nil {
		nodes = append(nodes, s.InputAll.Node)
	}
	s.Locker.Do(ctx, func() {
		s.OutputsMap.Range(func(_ SenderKey, output *Output[C]) bool {
			nodes = append(nodes, output.Nodes()...)
			return true
		})
	})
	return nodes
}

func (s *StreamMux[C]) GetCountersPtr() *nodetypes.Counters {
	inputStats := s.InputAll.Node.GetCountersPtr()

	return &nodetypes.Counters{
		Addressed: inputStats.Addressed,
		Missed:    inputStats.Missed,
		Received:  inputStats.Received,
		// TODO: add sum of all outputs as Sent
	}
}

// handleOutputNodeError processes an error from a node that carries
// OutputCustomData. Returns nil when the error has been fully handled
// (and must NOT be forwarded upward); returns the original error when
// the caller must forward it on errCh.
//
// In both branches the dead output is evicted from s.Outputs /
// s.OutputsMap and any OutputSwitch / OutputSyncer that had committed
// to it is demoted to math.MinInt32. Without active-branch eviction,
// getActiveVideoOutputLocked still resolves to the orphaned chain and
// AutoBitRateHandler.withActiveVideoOutput livelocks waiting for an
// "active" output whose serving goroutines have already exited
// (encoder stalls at 0 bps).
//
// The active-branch iteration runs BEFORE evictDeadOutput so the
// error-collection semantics (logging the input that owned this output,
// returning the underlying error from ForEachInput) are preserved.
func (s *StreamMux[C]) handleOutputNodeError(
	ctx context.Context,
	output *Output[C],
	nodeErr node.Error,
) error {
	// Check if the output is active on any input before deciding to close it.
	// In SplitAV mode, an output may be inactive for one input but active
	// for another; closing it would destroy the active data path.
	isActiveOnAnyInput := false
	if checkErr := s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		if int32(output.ID) == input.OutputSwitch.CurrentValue.Load() {
			isActiveOnAnyInput = true
		}
		return nil
	}); checkErr != nil {
		logger.Errorf(ctx, "unable to check active outputs: %v", checkErr)
	}

	var forwardErr error
	if isActiveOnAnyInput {
		forwardErr = s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
			if int32(output.ID) == input.OutputSwitch.CurrentValue.Load() {
				logger.Errorf(ctx, "error from the active output %d (%s) of input %s, node %T:%s: %v", output.ID, output.GetKey(), input.GetType(), nodeErr.Node, nodeErr.Node, nodeErr.Err)
				return nodeErr.Err
			}
			return nil
		})
		// Iteration above completed; demote AFTER so the iteration still
		// sees CurrentValue==output.ID and emits the error log.
		s.evictDeadOutput(ctx, output)
		return forwardErr
	}

	switch {
	case errors.Is(nodeErr.Err, io.EOF):
		logger.Debugf(ctx, "node <%T> received EOF, closing it", nodeErr.Node)
	case errors.Is(nodeErr.Err, context.Canceled):
		logger.Debugf(ctx, "node <%T> was canceled, closing it", nodeErr.Node)
	default:
		if r := processPlatformSpecificError(ctx, nodeErr.Err); r == nil {
			logger.Debugf(ctx, "got a platform-specific non-active output error %d: %v, closing it", output.ID, nodeErr.Err)
		} else {
			logger.Errorf(ctx, "got an error on a non-active output %d: %v, closing it", output.ID, nodeErr.Err)
		}
	}
	s.evictDeadOutput(ctx, output)
	if err := output.CloseNoDrain(ctx); err != nil {
		logger.Debugf(ctx, "unable to close output %d: %v", output.ID, err)
	}
	return nil
}

// evictDeadOutput removes a dead/closing output from every routing
// structure that could otherwise resolve back to it: the SenderKey-keyed
// OutputsMap, the OutputID-keyed Outputs map, and any OutputSwitch /
// OutputSyncer whose CurrentValue had committed to this output. Without
// this, getActiveVideoOutputLocked still returns the orphaned chain and
// AutoBitRateHandler.withActiveVideoOutput livelocks waiting for an
// "active" output whose serving goroutines have already exited.
//
// After the demotion to math.MinInt32, attempts to recommit each
// affected input to a surviving sibling output: without a recommit,
// OutputSyncer.Flags carrying SwitchFlagInactiveBlock would
// wedge every per-output Barrier on a StateBlock that has no drain
// target. The recommit goes through setPreferredOutputForInput so all
// the standard switch ceremony (force-keyframe on the new output, etc.)
// fires; if no sibling is reachable for a given input, the demoted
// state is left in place — the SwitchOutput.GetState defense-in-depth
// path (kernel/barrier/stategetter/switch.go) drops in that case rather
// than blocking.
//
// The caller still owns CloseNoDrain on the output; this method only
// detaches it from the lookup tables so a follow-up GetOrCreateOutput
// can stand up a fresh chain.
func (s *StreamMux[C]) evictDeadOutput(
	ctx context.Context,
	output *Output[C],
) {
	// StorageKey() (not GetKey()) is the authoritative OutputsMap key
	// here: GetKey() derives from the EncoderFactory state, which in
	// SplitAV mode picks up an extra Audio*/Video* axis once
	// reconfigureEncoder fills the factory with the full
	// TranscoderConfig. The CompareAndDelete keyed on the
	// post-reconfigure compound key would silently miss the entry
	// stored under the original split key, leaving a stale map entry
	// pointing at the dead output for the next eviction tick to trip
	// over.
	if s.OutputsMap.CompareAndDelete(output.StorageKey(), output) {
		logger.Debugf(ctx, "output %d removed from OutputsMap", output.ID)
	}
	if s.Outputs.CompareAndDelete(output.ID, output) {
		logger.Debugf(ctx, "output %d removed from Outputs", output.ID)
	}
	if foreachErr := s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		// Record lastEvictedKey BEFORE the OutputSwitch demote. A 1 Hz
		// retry tick running concurrently between the demote and a
		// later Store would read MinInt32, find no key, and skip,
		// costing one tick (~1 s) of recovery latency. Storing first
		// closes the race: any tick observing MinInt32 is guaranteed
		// to find the key.
		//
		// Stored unconditionally (not gated on the no-sibling branch):
		// when a sibling-failover succeeds, OutputSwitch advances back
		// off MinInt32 immediately, so the retry tick won't fire on
		// this input — the stale entry is harmless and is cleaned up
		// the next time the tick observes the recovered state (Delete
		// path in evictionRecreateRetryTick).
		willDemoteSwitch := input.OutputSwitch.CurrentValue.Load() == int32(output.ID)
		willDemoteSyncer := input.OutputSyncer.CurrentValue.Load() == int32(output.ID)
		if willDemoteSwitch || willDemoteSyncer {
			s.lastEvictedKey.Store(input, output.StorageKey())
		}
		demotedSwitch := input.OutputSwitch.CurrentValue.CompareAndSwap(int32(output.ID), math.MinInt32)
		if demotedSwitch {
			logger.Debugf(ctx, "demoted OutputSwitch.CurrentValue from %d to MinInt32 on input %s", output.ID, input.GetType())
		}
		demotedSyncer := input.OutputSyncer.CurrentValue.CompareAndSwap(int32(output.ID), math.MinInt32)
		if demotedSyncer {
			logger.Debugf(ctx, "demoted OutputSyncer.CurrentValue from %d to MinInt32 on input %s", output.ID, input.GetType())
		}
		if demotedSwitch || demotedSyncer {
			if hook := s.evictDemoteTestHook; hook != nil {
				hook(input)
			}
			s.recommitDemotedInputToSibling(ctx, input, output)
		}
		return nil
	}); foreachErr != nil {
		logger.Errorf(ctx, "unable to demote OutputSwitch/OutputSyncer for output %d: %v", output.ID, foreachErr)
	}
}

// recommitDemotedInputToSibling tries to point an input whose
// OutputSwitch / OutputSyncer was just demoted to MinInt32 (because its
// committed output errored and was evicted) at any surviving sibling
// output attached to the same input. Failure to find a sibling triggers
// the no-sibling recreate path: a fresh Output[N] is materialised
// under the dead output's SenderKey and the input is switched onto it.
// Without the recreate, typical single-video-output configs would stay
// wedged at MinInt32 — the SwitchOutput.GetState defense-in-depth
// check Drops cleanly, but with no replacement Output to drop INTO the
// encoder is gone for good and the camera path delivers 0 video
// packets.
//
// The recreate runs synchronously here for the on-eviction era. If it
// fails (or the input is still demoted afterwards) the 1 Hz retry tick
// (eviction_recreate_retry.go) takes over and keeps trying
// indefinitely — no backoff, no give-up. See RETRY_SEMANTICS.md.
func (s *StreamMux[C]) recommitDemotedInputToSibling(
	ctx context.Context,
	input *Input[C],
	deadOutput *Output[C],
) {
	// StorageKey() (not GetKey()) — see Output.StorageKey godoc and
	// evictDeadOutput's CompareAndDelete callsite. The recreate path
	// threads this key through setPreferredOutputs which must round-trip
	// through OutputsMap, and the eviction-recreate state map keys on
	// the same SenderKey across an eviction era.
	deadOutputKey := deadOutput.StorageKey()
	siblingKey, ok := s.findSiblingOutputKeyForInput(input, deadOutput)
	if !ok {
		s.handleNoSiblingEviction(ctx, input, deadOutput, deadOutputKey)
		return
	}
	if !s.IsAllowedDifferentOutputs() {
		// setPreferredOutputForInput rejects the call in non-different-
		// outputs modes; nothing to do — the demoted state is the
		// terminal state for this input.
		logger.Debugf(ctx, "MuxMode %s does not allow switching outputs; leaving demoted on input %s", s.MuxMode, input.GetType())
		return
	}
	err := s.setPreferredOutputForInput(ctx, input, siblingKey)
	switch {
	case err == nil:
		logger.Debugf(ctx, "recommitted input %s to sibling output %s after evicting %d", input.GetType(), siblingKey, deadOutput.ID)
	default:
		logger.Debugf(ctx, "unable to recommit input %s to sibling output %s after evicting %d: %v (defense-in-depth GetState will Drop)", input.GetType(), siblingKey, deadOutput.ID, err)
	}
}

// handleNoSiblingEviction is the no-sibling branch of
// recommitDemotedInputToSibling. It fires the recreate hook
// synchronously once for the orphaned input. lastEvictedKey is recorded
// upstream in evictDeadOutput BEFORE the OutputSwitch demote so the
// post-demote race window between demote and Store is closed (see the
// Store comment in evictDeadOutput).
//
// If the recreate fails, the input stays at OutputSwitch.CurrentValue
// == math.MinInt32 and the 1 Hz retry tick keeps trying indefinitely
// (eviction_recreate_retry.go). There is no backoff, no give-up
// point — see RETRY_SEMANTICS.md "What we deliberately do NOT do".
func (s *StreamMux[C]) handleNoSiblingEviction(
	ctx context.Context,
	input *Input[C],
	deadOutput *Output[C],
	deadOutputKey SenderKey,
) {
	if !s.IsAllowedDifferentOutputs() {
		logger.Warnf(ctx,
			"input %s is orphaned post-eviction of output %d (%s); MuxMode %s does not allow per-input switching, video output is 0 until a manual reconfigure",
			input.GetType(), deadOutput.ID, deadOutputKey, s.MuxMode)
		return
	}

	logger.Warnf(ctx,
		"input %s is orphaned post-eviction of output %d (%s); recreating fresh Output under same SenderKey",
		input.GetType(), deadOutput.ID, deadOutputKey)
	if err := s.recreateEvictedOutputFunc(ctx, input, deadOutputKey); err != nil {
		logger.Warnf(ctx,
			"unable to recreate output for orphaned input %s after evicting %d (%s): %v; the 1 Hz retry tick will keep trying",
			input.GetType(), deadOutput.ID, deadOutputKey, err)
		return
	}
	logger.Debugf(ctx,
		"recreated and re-attached output %s for orphaned input %s after evicting %d",
		deadOutputKey, input.GetType(), deadOutput.ID)
}

// findSiblingOutputKeyForInput scans s.Outputs for a non-closed output
// other than deadOutput that is attached to the same Input. Returns
// ok=false when no such sibling exists.
//
// Returns the candidate's StorageKey (not GetKey()) so the caller can
// round-trip back through OutputsMap.Load — see Output.StorageKey godoc
// for the GetKey()-vs-StorageKey divergence rationale.
func (s *StreamMux[C]) findSiblingOutputKeyForInput(
	input *Input[C],
	deadOutput *Output[C],
) (SenderKey, bool) {
	var foundKey SenderKey
	found := false
	s.Outputs.Range(func(_ OutputID, candidate *Output[C]) bool {
		if candidate == deadOutput {
			return true
		}
		if candidate.InputFrom != input.Node {
			return true
		}
		if candidate.IsClosed() {
			return true
		}
		foundKey = candidate.StorageKey()
		found = true
		return false
	})
	return foundKey, found
}
