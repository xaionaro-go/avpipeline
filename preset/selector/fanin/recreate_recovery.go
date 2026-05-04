package fanin

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

type RecreateTrigger uint8

const (
	RecreateTriggerNoFallback RecreateTrigger = iota + 1
	RecreateTriggerSwitchFailure
)

type RecreateRequest[K comparable] struct {
	Failure Failure[K]
	Trigger RecreateTrigger
}

type RecreateRecovery[K comparable, M Member] struct {
	Pair                  *switchpair.Pair
	Tracker               *orphanretry.Tracker[K]
	Recreate              orphanretry.RecreateFunc[K]
	RecordOnNoFallback    bool
	RecordOnSwitchFailure bool
	SafeKeyFormatter      safekey.Formatter[K]
}

func (r *RecreateRecovery[K, M]) Recover(
	ctx context.Context,
	req RecreateRequest[K],
) error {
	if r == nil || !r.shouldRecord(req.Trigger) {
		return nil
	}
	if r.Pair == nil {
		return selectorerr.InvalidConfig("recreate.Pair", ErrMissingPair)
	}
	if r.Tracker == nil {
		return selectorerr.InvalidConfig("recreate.Tracker", ErrMissingTracker)
	}
	if r.Recreate == nil {
		return selectorerr.InvalidConfig("recreate.Recreate", ErrMissingRecreate)
	}

	r.Tracker.RecordDemotion(ctx, req.Failure.RouteID, req.Failure.StorageKey)
	r.Pair.DemoteIfCurrent(ctx, req.Failure.MemberID)

	if err := r.Recreate(ctx, req.Failure.RouteID, req.Failure.StorageKey); err != nil {
		return fmt.Errorf(
			"recreate route %q storage key %s: %w",
			req.Failure.RouteID,
			safekey.Format(ctx, r.SafeKeyFormatter, req.Failure.StorageKey),
			err,
		)
	}
	if r.Pair.Current(ctx) != id.NoMemberID {
		r.Tracker.MarkRecovered(ctx, req.Failure.RouteID)
	}

	return nil
}

func (r *RecreateRecovery[K, M]) shouldRecord(
	trigger RecreateTrigger,
) bool {
	switch trigger {
	case RecreateTriggerNoFallback:
		return r.RecordOnNoFallback
	case RecreateTriggerSwitchFailure:
		return r.RecordOnSwitchFailure
	default:
		return false
	}
}
