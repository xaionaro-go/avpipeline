package fanin

import (
	"context"
	"sync"

	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
)

type Controller[K comparable, M Member] struct {
	lock             sync.Mutex
	routeID          id.RouteID
	pair             *switchpair.Pair
	members          *member.Registry[K, M]
	gate             *switchprogress.Gate
	asyncErrors      AsyncErrorHandler
	availability     map[id.MemberID]availability.Candidate
	priorityPolicy   *PriorityPolicy[K, M]
	pausePlanner     *PausePlanner[K, M]
	openPromotion    *OpenPromotion[K, M]
	fallbackHandler  *FallbackHandler[K, M]
	recreateRecovery *RecreateRecovery[K, M]
	retryTracker     *orphanretry.Tracker[K]
}

type controllerSnapshot[K comparable, M Member] struct {
	current         id.MemberID
	next            id.MemberID
	entries         map[id.MemberID]member.Entry[K, M]
	candidates      []PriorityCandidate[K, M]
	priorities      []id.MemberID
	paused          map[id.MemberID]bool
	unpausedCount   int
	previousPending id.MemberID
}

func New[K comparable, M Member](
	_ context.Context,
	cfg Config[K, M],
) (*Controller[K, M], error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	var tracker *orphanretry.Tracker[K]
	var err error
	if cfg.Recreate != nil {
		tracker, err = configuredTracker(*cfg.Recreate)
		if err != nil {
			return nil, err
		}
	}

	priorityPolicy := defaultPriorityPolicy(cfg.PriorityPolicy)
	controller := &Controller[K, M]{
		routeID:          cfg.RouteID,
		pair:             cfg.Pair,
		members:          cfg.Members,
		gate:             cfg.Gate,
		asyncErrors:      cfg.AsyncErrors,
		availability:     map[id.MemberID]availability.Candidate{},
		priorityPolicy:   priorityPolicy,
		pausePlanner:     defaultPausePlanner(cfg.PausePlanner),
		openPromotion:    defaultOpenPromotion(cfg.OpenPromotion),
		fallbackHandler:  defaultFallbackHandler(cfg.FallbackHandler, priorityPolicy),
		recreateRecovery: cfg.RecreateRecovery,
		retryTracker:     tracker,
	}
	if controller.recreateRecovery == nil {
		controller.recreateRecovery = newRecreateRecovery(cfg, tracker)
	}

	return controller, nil
}

func (c *Controller[K, M]) AddMember(
	ctx context.Context,
	priority id.MemberID,
	storageKey K,
	value M,
	source availability.Source,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	entry, err := c.members.Put(ctx, priority, storageKey, value)
	if err != nil {
		return err
	}
	c.availability[entry.ID] = candidateAvailability(source)

	return nil
}

func (c *Controller[K, M]) PauseMember(
	ctx context.Context,
	priority id.MemberID,
) error {
	snapshot, err := c.snapshotForMember(ctx, priority)
	if err != nil {
		return err
	}

	plan, err := c.pausePlanner.PlanPause(ctx, snapshot.pauseState(priority), priority)
	if err != nil {
		return err
	}

	return c.applyLifecycle(ctx, snapshot.entries, plan.PauseAfterSwitch, lifecyclePause)
}

func (c *Controller[K, M]) UnpauseMember(
	ctx context.Context,
	priority id.MemberID,
) error {
	snapshot, err := c.snapshotForMember(ctx, priority)
	if err != nil {
		return err
	}

	plan, err := c.pausePlanner.PlanUnpause(ctx, snapshot.pauseState(priority), priority)
	if err != nil {
		return err
	}

	return c.applyLifecycle(ctx, snapshot.entries, plan.UnpauseBeforeSwitch, lifecycleUnpause)
}

func (c *Controller[K, M]) OnMemberOpen(
	ctx context.Context,
	priority id.MemberID,
) error {
	snapshot, err := c.snapshotForMember(ctx, priority)
	if err != nil {
		return err
	}

	decision, err := c.openPromotion.PlanOpen(ctx, snapshot.current, priority, snapshot.candidates)
	if err != nil {
		return err
	}
	if !decision.Promote {
		return nil
	}

	if err := c.switchTo(ctx, decision.Target, nil); err != nil {
		return err
	}
	c.markRecovered(ctx)

	return nil
}

func (c *Controller[K, M]) OnMemberError(
	ctx context.Context,
	priority id.MemberID,
	cause error,
) error {
	current := c.pair.Current(ctx)
	next := c.pair.Next(ctx)
	if priority != current && priority != next {
		return nil
	}

	snapshot, err := c.snapshotForMember(ctx, priority)
	if err != nil {
		return err
	}
	entry := snapshot.entries[priority]
	failure := Failure[K]{
		RouteID:    c.routeID,
		MemberID:   priority,
		StorageKey: entry.StorageKey,
		Current:    current,
		Next:       next,
		Cause:      cause,
	}

	decision, err := c.fallbackHandler.HandleFailure(ctx, failure, snapshot.candidates)
	if err != nil {
		return err
	}
	if decision.IgnoreFailure {
		return nil
	}
	if decision.UseRecreate {
		return c.recreateRecovery.Recover(ctx, RecreateRequest[K]{
			Failure: failure,
			Trigger: RecreateTriggerNoFallback,
		})
	}

	if err := c.switchTo(ctx, decision.SwitchTo, &failure); err != nil {
		return err
	}
	c.markRecovered(ctx)

	return nil
}

func (c *Controller[K, M]) snapshotForMember(
	ctx context.Context,
	priority id.MemberID,
) (controllerSnapshot[K, M], error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	snapshot := c.snapshotLocked(ctx)
	if _, ok := snapshot.entries[priority]; !ok {
		return controllerSnapshot[K, M]{}, selectorerr.MemberNotFound(priority)
	}

	return snapshot, nil
}

func (c *Controller[K, M]) snapshotLocked(
	ctx context.Context,
) controllerSnapshot[K, M] {
	snapshot := controllerSnapshot[K, M]{
		current:         c.pair.Current(ctx),
		next:            c.pair.Next(ctx),
		entries:         map[id.MemberID]member.Entry[K, M]{},
		paused:          map[id.MemberID]bool{},
		previousPending: c.pair.Next(ctx),
	}
	c.members.Range(ctx, func(entry member.Entry[K, M]) bool {
		paused := entry.Value.IsPaused(ctx)
		snapshot.entries[entry.ID] = entry
		snapshot.paused[entry.ID] = paused
		snapshot.priorities = append(snapshot.priorities, entry.ID)
		resource, ok := c.availability[entry.ID]
		if !ok {
			resource = availability.Present(nil)
		}
		snapshot.candidates = append(snapshot.candidates, PriorityCandidate[K, M]{
			Priority:     entry.ID,
			Entry:        entry,
			Availability: resource,
		})
		if !paused {
			snapshot.unpausedCount++
		}
		return true
	})

	return snapshot
}

func (s controllerSnapshot[K, M]) pauseState(
	target id.MemberID,
) PauseState {
	return PauseState{
		Current:         s.current,
		PreviousPending: s.previousPending,
		Target:          target,
		UnpausedCount:   s.unpausedCount,
		Priorities:      s.priorities,
		Paused:          s.paused,
	}
}

func (c *Controller[K, M]) applyLifecycle(
	ctx context.Context,
	entries map[id.MemberID]member.Entry[K, M],
	priorities []id.MemberID,
	operation lifecycleOperation,
) error {
	for _, priority := range priorities {
		entry, ok := entries[priority]
		if !ok {
			continue
		}
		if err := runLifecycle(ctx, entry.Value, operation); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller[K, M]) markRecovered(
	ctx context.Context,
) {
	if c.retryTracker == nil {
		return
	}
	c.retryTracker.MarkRecovered(ctx, c.routeID)
}

func defaultPriorityPolicy[K comparable, M Member](
	policy *PriorityPolicy[K, M],
) *PriorityPolicy[K, M] {
	if policy != nil {
		return policy
	}
	return &PriorityPolicy[K, M]{}
}

func defaultPausePlanner[K comparable, M Member](
	planner *PausePlanner[K, M],
) *PausePlanner[K, M] {
	if planner != nil {
		return planner
	}
	return &PausePlanner[K, M]{}
}

func defaultOpenPromotion[K comparable, M Member](
	promotion *OpenPromotion[K, M],
) *OpenPromotion[K, M] {
	if promotion != nil {
		return promotion
	}
	return &OpenPromotion[K, M]{}
}

func defaultFallbackHandler[K comparable, M Member](
	handler *FallbackHandler[K, M],
	priorityPolicy *PriorityPolicy[K, M],
) *FallbackHandler[K, M] {
	if handler != nil {
		ret := *handler
		if ret.PriorityPolicy == nil {
			ret.PriorityPolicy = priorityPolicy
		}
		return &ret
	}
	return &FallbackHandler[K, M]{
		PriorityPolicy: priorityPolicy,
	}
}

func newRecreateRecovery[K comparable, M Member](
	cfg Config[K, M],
	tracker *orphanretry.Tracker[K],
) *RecreateRecovery[K, M] {
	if cfg.Recreate == nil {
		return &RecreateRecovery[K, M]{
			Pair:             cfg.Pair,
			SafeKeyFormatter: cfg.SafeKeyFormatter,
		}
	}

	return &RecreateRecovery[K, M]{
		Pair:                  cfg.Pair,
		Tracker:               tracker,
		Recreate:              cfg.Recreate.Recreate,
		RecordOnNoFallback:    cfg.Recreate.RecordOnNoFallback,
		RecordOnSwitchFailure: cfg.Recreate.RecordOnSwitchFailure,
		SafeKeyFormatter:      cfg.SafeKeyFormatter,
	}
}
