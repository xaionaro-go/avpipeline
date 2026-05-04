package fanout

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestSetPreferredRunsSwitchHooksOutsideControllerLock(t *testing.T) {
	ctx := context.Background()
	const routeID = id.RouteID("all")
	const initialID = id.MemberID(1)
	const targetID = id.MemberID(7)
	const targetKey = "target"

	routes := route.NewRegistry()
	members := member.NewRegistry[string, controllerLockTestMember]()
	attachments := attachment.NewIndex(func(
		ctx context.Context,
		routeID id.RouteID,
	) bool {
		_, ok := routes.Load(ctx, routeID)
		return ok
	})
	var controller *Controller[string, controllerLockTestMember]
	hookCalled := false
	errControllerLockHeld := errors.New("controller lock held through switch hook")
	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue: initialID,
		Hooks: switchpair.Hooks{
			OnSwitchRequest: func(context.Context, id.MemberID) error {
				hookCalled = true
				if !controller.lock.TryLock() {
					return errControllerLockHeld
				}
				controller.lock.Unlock()
				return nil
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, routes.Add(ctx, route.State{ID: routeID, Pair: pair}))
	_, err = members.Put(ctx, targetID, targetKey, controllerLockTestMember{})
	require.NoError(t, err)
	require.NoError(t, attachments.Attach(ctx, routeID, targetID))

	controller, err = New(ctx, Config[string, controllerLockTestMember]{
		Routes:      routes,
		Members:     members,
		Attachments: attachments,
		MemberIDs:   controllerLockAllocator{},
		CreationPlanner: CreationPlannerFunc[string](func(
			context.Context,
			string,
			[]ExistingMember[string],
		) (CreationDecision[string], error) {
			return CreationDecision[string]{Action: CreationActionReject}, nil
		}),
		PreferredRoutePlanner: PreferredRoutePlannerFunc[string](func(
			context.Context,
			string,
		) ([]RoutePlan[string], error) {
			return []RoutePlan[string]{{RouteID: routeID, StorageKey: targetKey}}, nil
		}),
		DifferentOutputPolicy: DifferentOutputPolicyFunc(func(context.Context) bool {
			return true
		}),
		PreferenceSwitcher: NewPreferenceSwitcher[string, controllerLockTestMember](nil),
		Eviction:           controllerLockEviction{},
	})
	require.NoError(t, err)

	err = controller.SetPreferred(ctx, targetKey)
	require.NoError(t, err)
	require.True(t, hookCalled)
	require.Equal(t, targetID, pair.Current(ctx))
}

type controllerLockTestMember struct{}

type controllerLockAllocator struct{}

func (controllerLockAllocator) Allocate(
	context.Context,
) (id.MemberID, error) {
	return id.NoMemberID, nil
}

type controllerLockEviction struct{}

func (controllerLockEviction) Evict(
	context.Context,
	member.Entry[string, controllerLockTestMember],
) (eviction.Result, error) {
	return eviction.Result{}, nil
}
