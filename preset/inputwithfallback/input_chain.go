// input_chain.go implements a single input chain in the fallback preset.

package inputwithfallback

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	selectorresetter "github.com/xaionaro-go/avpipeline/preset/selector/resetter"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

type InputID int

type InputNode[K InputKernel, C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Retryable[K]]]

type InputNodes[K InputKernel, C any] []*InputNode[K, C]

func (in InputNodes[K, C]) NonNil() InputNodes[K, C] {
	var r InputNodes[K, C]
	for _, n := range in {
		if n != nil {
			r = append(r, n)
		}
	}
	return r
}

type InputKernel interface {
	kernel.Abstract
	packet.Source
}

var _ InputKernel = (*kernel.Input)(nil)

// AvailabilityFactory returns the InputChain's InputFactory typed as
// any so the SSOT WalkAvailableAfter helper can perform the optional
// InputFactoryWithAvailability assertion uniformly across packages.
// See walk_available_after.go for the contract.
func (i *InputChain[K, DF, C]) AvailabilityFactory() any {
	if i == nil {
		return nil
	}
	return i.InputFactory
}

// InputChain represents a single input chain in the fallback preset.
// retryable:input -> filter (-> autoheaders -> decoder) -> syncBarrier
type InputChain[K InputKernel, DF codec.DecoderFactory, C any] struct {
	ID           InputID
	InputFactory InputFactory[K, DF, C]
	Input        *InputNode[K, C]
	FilterSwitch *barrierstategetter.SwitchOutput
	Filter       *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	AutoHeaders  *autoheaders.NodeWithCustomData[C]
	Decoder      *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Decoder[DF]]]
	SyncSwitch   *barrierstategetter.SwitchOutput
	SyncBarrier  *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	isServing    atomic.Bool
	// openCount tracks how many times the upstream Retryable has
	// successfully opened its inner kernel. The first open sets up
	// virgin downstream kernels (no Reset needed); every subsequent
	// open is a reconnect, requiring downstream Decoder/AutoHeaders/
	// Filter Reset so they shed state observed against the prior
	// connection. See resetDownstreamKernels.
	openCount atomic.Uint64
	// resetDownstreamKernelsTimeout caps how long each per-processor
	// Reset call inside resetDownstreamKernels may block before being
	// abandoned. Zero or negative means "use package default" — see
	// option.go::defaultResetDownstreamKernelsTimeout.
	resetDownstreamKernelsTimeout time.Duration
}

func newInputChain[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputID InputID,
	inputFactory InputFactory[K, DF, C],
	filterSwitch *barrierstategetter.SwitchOutput,
	syncSwitch *barrierstategetter.SwitchOutput,
	quietOnOpenFailure bool,
	resetDownstreamKernelsTimeout time.Duration,
	onKernelOpen func(context.Context, *InputChain[K, DF, C]),
	onError func(context.Context, *InputChain[K, DF, C], error) error,
) (*InputChain[K, DF, C], error) {
	r := &InputChain[K, DF, C]{
		ID:                            inputID,
		InputFactory:                  inputFactory,
		FilterSwitch:                  filterSwitch,
		Filter:                        node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(ctx, filterSwitch)),
		SyncSwitch:                    syncSwitch,
		SyncBarrier:                   node.NewWithCustomDataFromKernel[C](ctx, kernel.NewBarrier(ctx, syncSwitch)),
		resetDownstreamKernelsTimeout: resetDownstreamKernelsTimeout,
	}

	decoderFactory, err := inputFactory.NewDecoderFactory(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("unable to create decoder factory for input %v: %w", inputID, err)
	}
	if any(decoderFactory) != codec.DecoderFactory(nil) {
		r.Decoder = node.NewWithCustomDataFromKernel[C](
			ctx,
			kernel.NewDecoder(ctx, decoderFactory),
			processor.DefaultOptionsTranscoder()...,
		)
	}

	inputKernel := kernel.NewRetryable(ctx,
		func(ctx context.Context) (K, error) {
			return inputFactory.NewInput(ctx, r)
		},
		func(ctx context.Context, k K, err error) error {
			// QuietOnOpenFailure covers two by-design noise patterns
			// at this site:
			//   (a) Empty-priority slot — the factory reports
			//       HasResources=false; NewInput errors with "no input
			//       resources configured for priority N".
			//   (b) Configured-but-unavailable upstream — the factory
			//       has resources (HasResources=true), e.g. an rtmp
			//       URL, but the upstream publisher hasn't connected
			//       yet so libav OpenInput fails (I/O error,
			//       connection refused, EOF, "no one is publishing
			//       to <path>"). The retry loop fires this every
			//       RetryInterval until the publisher appears; at
			//       steady state this is normal, not an error.
			// Both arms demote to Debug under the flag; the operator
			// opted in via -quiet_on_open_failure (legacy alias
			// -quiet_empty_priority) and accepted that a genuinely
			// typo'd URL also stays at Debug. Default (flag off) keeps
			// Errorf.
			if quietOnOpenFailure {
				logger.Debugf(ctx, "input %v error: %v", inputID, err)
			} else {
				logger.Errorf(ctx, "input %v error: %v", inputID, err)
			}
			if onError != nil {
				if err := onError(ctx, r, err); err != nil {
					return err
				}
			}
			return kernel.ErrRetry{}
		},
		kernel.RetryableOptionStartOnInit[K](false),
		kernel.RetryableOptionOnKernelOpen[K](func(
			ctx context.Context,
			k K,
		) (_err error) {
			logger.Debugf(ctx, "RetryableOptionOnKernelOpen: %d", inputID)
			defer func() { logger.Debugf(ctx, "/RetryableOptionOnKernelOpen: %d: %v", inputID, _err) }()
			// On every reopen (i.e. count >= 2 — the first open is
			// virgin), reset downstream kernels so they shed state
			// observed against the prior connection. Without this,
			// the Decoder's per-stream codec contexts, AutoHeaders'
			// IsSet flag, and the Filter SwitchOutput's PTS bridge
			// continue to assume the prior connection's stream
			// parameters, blocking video flow on every reconnect.
			if r.openCount.Add(1) > 1 {
				if err := r.resetDownstreamKernels(ctx); err != nil {
					logger.Errorf(ctx, "RetryableOptionOnKernelOpen: input %d: unable to reset downstream kernels: %v", inputID, err)
				}
			}
			if onKernelOpen != nil {
				onKernelOpen(ctx, r)
			}
			return nil
		}),
	)
	r.Input = node.NewWithCustomDataFromKernel[C](
		ctx,
		inputKernel,
		processor.DefaultOptionsInput()...,
	)
	r.Input.AddPushTo(ctx, r.Filter, r.onInput())
	if r.Decoder == nil {
		r.Filter.AddPushTo(ctx, r.SyncBarrier)
	} else {
		r.AutoHeaders = autoheaders.NewNodeWithCustomData[C](ctx, r.Decoder.Processor.Kernel)
		r.Filter.AddPushTo(ctx, r.AutoHeaders)
		r.AutoHeaders.AddPushTo(ctx, r.Decoder)
		r.Decoder.AddPushTo(ctx, r.SyncBarrier)
	}

	return r, nil
}

func (i *InputChain[K, DF, C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	if !i.isServing.CompareAndSwap(false, true) {
		panic("InputChain.Serve: already started")
	}
	defer i.isServing.Store(false)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	logger.Debugf(ctx, "InputChain[%d].Serve: started", i.ID)
	defer logger.Debugf(ctx, "InputChain[%d].Serve: ended", i.ID)

	var wg sync.WaitGroup

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: input node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: input node serving started", i.ID)
		i.Input.Serve(ctx, cfg, errCh)
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: filter node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: filter node serving started", i.ID)
		i.Filter.Serve(ctx, cfg, errCh)
	})

	if i.AutoHeaders != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Debugf(ctx, "InputChain[%d].Serve: autoheaders node serving ended", i.ID)
			logger.Debugf(ctx, "InputChain[%d].Serve: autoheaders node serving started", i.ID)
			i.AutoHeaders.Serve(ctx, cfg, errCh)
		})
	}

	if i.Decoder != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving ended", i.ID)
			logger.Debugf(ctx, "InputChain[%d].Serve: decoder node serving started", i.ID)
			i.Decoder.Serve(ctx, cfg, errCh)
		})
	}

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		defer logger.Debugf(ctx, "InputChain[%d].Serve: sync barrier node serving ended", i.ID)
		logger.Debugf(ctx, "InputChain[%d].Serve: sync barrier node serving started", i.ID)
		i.SyncBarrier.Serve(ctx, cfg, errCh)
	})

	wg.Wait()
}

func (i *InputChain[K, DF, C]) String() string {
	ctx := context.Background()
	if !i.Input.Processor.Kernel.KernelLocker.ManualTryLock(ctx) {
		return fmt.Sprintf("InputChain(<unable to lock>; factory:%s)", i.InputFactory)
	}
	kernel := func() K {
		defer i.Input.Processor.Kernel.KernelLocker.ManualUnlock(ctx)
		return i.Input.Processor.Kernel.Kernel
	}()
	if i.Input.Processor.Kernel.KernelIsSet {
		return fmt.Sprintf("InputChain(%v:active)", kernel)
	}
	return fmt.Sprintf("InputChain(%s:inactive)", i.InputFactory)
}

func (i *InputChain[K, DF, C]) Unpause(
	ctx context.Context,
) error {
	return i.Input.Processor.Kernel.Unpause(ctx)
}

func (i *InputChain[K, DF, C]) Pause(
	ctx context.Context,
) error {
	return i.Input.Processor.Kernel.Pause(ctx)
}

// IsKernelOpen forwards to Retryable.IsKernelOpen — true only when the
// underlying input kernel is fully opened and not yet closed. Use this
// to distinguish "open in flight" from "open and serving" when a caller
// must avoid Pause+Unpause kicks against a freshly-opening kernel.
func (i *InputChain[K, DF, C]) IsKernelOpen(ctx context.Context) bool {
	return i.Input.Processor.Kernel.IsKernelOpen(ctx)
}

func (i *InputChain[K, DF, C]) GetInput() node.Abstract {
	return i.Input
}

func (i *InputChain[K, DF, C]) GetOutput() node.Abstract {
	return i.SyncBarrier
}

func (i *InputChain[K, DF, C]) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()

	var errs []error
	if err := i.Input.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input node: %w", err))
	}
	if err := i.Filter.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close filter node: %w", err))
	}
	if i.AutoHeaders != nil {
		if err := i.AutoHeaders.Processor.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close autoheaders node: %w", err))
		}
	}
	if i.Decoder != nil {
		if err := i.Decoder.Processor.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close decoder node: %w", err))
		}
	}
	if err := i.SyncBarrier.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close sync barrier node: %w", err))
	}
	return errors.Join(errs...)
}

func (i *InputChain[K, DF, C]) IsPaused(
	ctx context.Context,
) bool {
	return i.Input.Processor.Kernel.IsPaused(ctx)
}

// namedResetter pairs a Resetter with a stable human-readable name for
// log/error context inside resetDownstreamKernels and its testable
// inner helper.
type namedResetter struct {
	name string
	r    kerneltypes.Resetter
}

// resetDownstreamKernels invokes Reset on every Resetter-capable
// downstream Processor in this input chain (Filter Barrier,
// AutoHeaders, Decoder, SyncBarrier). It is invoked from the Retryable
// OnKernelOpen callback on every reopen so that those processors:
//
//  1. Drain stale packets observed against the prior connection from
//     their buffered InputCh / preOutputCh / OutputCh — without this,
//     stale packets back-pressure the chain and the upstream pusher
//     hits "queue is full (size: 1)" once fresh packets arrive.
//  2. Re-derive kernel observation state (e.g. Decoder's per-stream
//     codec contexts, AutoHeaders' detection state) against the
//     freshly-opened upstream.
//
// FromKernel.Reset performs (1) directly on its own queues and forwards
// to the wrapped kernel's Reset for (2). Walking the Processor (rather
// than just Processor.Kernel) is what closes the output-side wedge.
//
// Order: from upstream to downstream so each Reset sees the prior
// kernel's freshly-cleared state if the implementations ever consult
// each other.
//
// Containment timeout: see runResetters.
func (i *InputChain[K, DF, C]) resetDownstreamKernels(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "resetDownstreamKernels[%d]", i.ID)
	defer func() { logger.Debugf(ctx, "/resetDownstreamKernels[%d]: %v", i.ID, _err) }()

	resetters := []namedResetter{
		{"Filter", i.Filter.Processor},
	}
	if i.AutoHeaders != nil {
		// AutoHeaders' Processor is *FromKernel[*Base[*AutoHeaders]] —
		// the handler keeps detection state internal (h.SelectedKernel)
		// instead of swapping the Processor.Kernel pointer, which avoids
		// racing with the processor reader-loop's unlocked snapshot.
		// Reset on the Processor drains its queues and forwards to the
		// wrapped kernel's Reset (no-op if the kernel doesn't implement
		// Resetter).
		resetters = append(resetters, namedResetter{"AutoHeaders", i.AutoHeaders.Processor})
	}
	if i.Decoder != nil {
		resetters = append(resetters, namedResetter{"Decoder", i.Decoder.Processor})
	}
	if i.SyncBarrier != nil {
		resetters = append(resetters, namedResetter{"SyncBarrier", i.SyncBarrier.Processor})
	}

	timeout := i.resetDownstreamKernelsTimeout
	if timeout <= 0 {
		timeout = defaultResetDownstreamKernelsTimeout
	}

	return runResetters(ctx, int(i.ID), timeout, resetters)
}

// runResetters is the testable inner of resetDownstreamKernels. It
// invokes nr.Reset for each entry under a per-call context.WithTimeout
// boundary; on timeout the entry is skipped with a Warn log and the
// loop continues. Errors from non-timed-out Resets are accumulated;
// one failure does not skip the remaining entries.
//
// Containment-timeout rationale: a wedged downstream Reset must NOT
// block the OnKernelOpen path beyond the configured budget. We check
// ctx.Err() on the per-call context after the Reset returns rather
// than relying on the Reset return value, because xsync.DoA2R1 (the
// path most Resets take to acquire their own internal locker) honors
// context cancellation by returning the locked function's zero value
// (nil error) — i.e. a timed-out Reset looks like a successful no-op
// from the return value alone. The outer ctx.Err() guard avoids
// false-positive timeout reports when the caller itself is cancelled.
//
// inputID is the integer chain ID, accepted as int (rather than the
// generic InputID) so the helper has no type-parameter dependency and
// can be tested with hand-rolled Resetter mocks.
func runResetters(
	ctx context.Context,
	inputID int,
	timeout time.Duration,
	resetters []namedResetter,
) error {
	selectorResetters := make([]selectorresetter.Named, 0, len(resetters))
	for _, nr := range resetters {
		selectorResetters = append(selectorResetters, selectorresetter.Named{
			Name:     nr.name,
			Resetter: nr.r,
		})
	}
	return selectorresetter.Run(ctx, fmt.Sprintf("input %d", inputID), timeout, selectorResetters)
}
