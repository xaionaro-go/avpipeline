package fanin

import (
	"context"
	"errors"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
)

func (c *Controller[K, M]) SwitchTo(
	ctx context.Context,
	priority id.MemberID,
) error {
	return c.switchTo(ctx, priority, nil)
}

func (c *Controller[K, M]) switchTo(
	ctx context.Context,
	priority id.MemberID,
	failure *Failure[K],
) error {
	snapshot, err := c.snapshotForMember(ctx, priority)
	if err != nil {
		return err
	}

	c.gate.SupersedeStuckCycle()
	work, err := c.gate.StartRequest(priority)
	if err != nil {
		return err
	}
	defer work.Release()

	plan, err := c.pausePlanner.PlanSwitch(ctx, snapshot.pauseState(priority))
	if err != nil {
		return err
	}

	c.applyLifecycleAsync(ctx, snapshot.entries, plan.UnpauseBeforeSwitch, lifecycleUnpause)
	c.applyLifecycleAsync(ctx, snapshot.entries, plan.PausePreviousPending, lifecyclePause)

	if err := c.pair.SetValue(ctx, priority); err != nil {
		if failure == nil {
			return err
		}
		recreateErr := c.recreateRecovery.Recover(ctx, RecreateRequest[K]{
			Failure: *failure,
			Trigger: RecreateTriggerSwitchFailure,
		})
		return errors.Join(err, recreateErr)
	}

	if c.pair.Current(ctx) == priority {
		c.applyLifecycleAsync(ctx, snapshot.entries, plan.PauseAfterSwitch, lifecyclePause)
	}

	return nil
}

type lifecycleOperation uint8

const (
	lifecyclePause lifecycleOperation = iota + 1
	lifecycleUnpause
)

func (c *Controller[K, M]) applyLifecycleAsync(
	ctx context.Context,
	entries map[id.MemberID]member.Entry[K, M],
	priorities []id.MemberID,
	operation lifecycleOperation,
) {
	for _, priority := range priorities {
		entry, ok := entries[priority]
		if !ok {
			continue
		}
		if err := runLifecycle(ctx, entry.Value, operation); err != nil {
			c.asyncErrors(ctx, err)
		}
	}
}

func runLifecycle(
	ctx context.Context,
	member Member,
	operation lifecycleOperation,
) error {
	switch operation {
	case lifecyclePause:
		return member.Pause(ctx)
	case lifecycleUnpause:
		return member.Unpause(ctx)
	default:
		return nil
	}
}
