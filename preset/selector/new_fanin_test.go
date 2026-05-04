package selector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector"
	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanin"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
)

func TestNewFanInDelegatesToRealConstructor(t *testing.T) {
	ctx := context.Background()
	controller, err := selector.NewFanIn(ctx, validFanInConfig(t, ctx))
	require.NoError(t, err)
	require.NotNil(t, controller)

	err = controller.AddMember(ctx, id.MemberID(1), "primary", &rootFanInMember{}, availability.Source(nil))
	require.NoError(t, err)
}

func TestNewFanInWrapsInvalidConfigWithRootAliasAndPreservesCause(t *testing.T) {
	ctx := context.Background()

	controller, err := selector.NewFanIn[string, *rootFanInMember](ctx, fanin.Config[string, *rootFanInMember]{})
	require.Nil(t, controller)
	require.ErrorIs(t, err, selector.ErrInvalidConfig)
	require.ErrorIs(t, err, fanin.ErrMissingPair)
	require.ErrorIs(t, err, fanin.ErrMissingMembers)
	require.True(t, errors.Is(err, selector.ErrInvalidConfig))
	require.Contains(t, err.Error(), "fanin")
}

func validFanInConfig(
	t *testing.T,
	ctx context.Context,
) fanin.Config[string, *rootFanInMember] {
	t.Helper()

	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue: id.NoMemberID,
	})
	require.NoError(t, err)

	return fanin.Config[string, *rootFanInMember]{
		RouteID:     id.RouteID("input"),
		Pair:        pair,
		Members:     member.NewRegistry[string, *rootFanInMember](),
		Gate:        &switchprogress.Gate{},
		AsyncErrors: func(context.Context, error) {},
	}
}

type rootFanInMember struct {
	paused bool
}

func (m *rootFanInMember) Pause(context.Context) error {
	m.paused = true
	return nil
}

func (m *rootFanInMember) Unpause(context.Context) error {
	m.paused = false
	return nil
}

func (m *rootFanInMember) IsPaused(context.Context) bool {
	return m.paused
}
