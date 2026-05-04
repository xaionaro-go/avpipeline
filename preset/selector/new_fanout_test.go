package selector_test

import (
	"context"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

func TestNewFanOutDelegatesToRealConstructor(t *testing.T) {
	ctx := context.Background()
	controller, err := selector.NewFanOut(ctx, validFanOutConfig())
	require.NoError(t, err)
	require.NotNil(t, controller)

	pair, err := switchpair.New(ctx, switchpair.Config{
		InitialValue: id.NoMemberID,
	})
	require.NoError(t, err)
	require.NoError(t, controller.AddRoute(ctx, id.RouteID("all"), pair))

	entry, decision, err := controller.AddMember(ctx, "target", rootFanOutMember{})
	require.NoError(t, err)
	require.Equal(t, fanout.CreationActionCreate, decision.Action)
	require.Equal(t, id.MemberID(1), entry.ID)
	require.Equal(t, "target", entry.StorageKey)
}

func TestNewFanOutWrapsInvalidConfigWithRootAliasAndPreservesCause(t *testing.T) {
	ctx := context.Background()

	controller, err := selector.NewFanOut[string, rootFanOutMember](ctx, fanout.Config[string, rootFanOutMember]{})
	require.Nil(t, controller)
	require.ErrorIs(t, err, selector.ErrInvalidConfig)
	require.ErrorIs(t, err, fanout.ErrMissingRoutes)
	require.ErrorIs(t, err, fanout.ErrMissingCreationPlanner)
	require.Contains(t, err.Error(), "fanout")
}

func TestRootConstructorImportsStayLight(t *testing.T) {
	allowed := map[string]struct{}{
		"context": {},
		"errors":  {},
		"github.com/xaionaro-go/avpipeline/preset/selector/fanin":       {},
		"github.com/xaionaro-go/avpipeline/preset/selector/fanout":      {},
		"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr": {},
	}

	for _, filename := range []string{"new_fanin.go", "new_fanout.go"} {
		t.Run(filename, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			require.NoError(t, err)

			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				require.NoError(t, err)
				require.Contains(t, allowed, path)
			}
		})
	}
}

func validFanOutConfig() fanout.Config[string, rootFanOutMember] {
	routes := route.NewRegistry()
	return fanout.Config[string, rootFanOutMember]{
		Routes:  routes,
		Members: member.NewRegistry[string, rootFanOutMember](),
		Attachments: attachment.NewIndex(func(
			ctx context.Context,
			routeID id.RouteID,
		) bool {
			_, ok := routes.Load(ctx, routeID)
			return ok
		}),
		MemberIDs:             &rootFanOutAllocator{next: id.MemberID(1)},
		CreationPlanner:       fanout.NewCreationPlanner[string](fanout.ModeDifferentOutputsSameTracks, id.RouteID("all")),
		PreferredRoutePlanner: fanout.NewRoutePlanner[string](fanout.ModeDifferentOutputsSameTracks, id.RouteID("all")),
		DifferentOutputPolicy: fanout.NewDifferentOutputPolicy(fanout.ModeDifferentOutputsSameTracks),
		PreferenceSwitcher:    fanout.NewPreferenceSwitcher[string, rootFanOutMember](nil),
		Eviction:              rootFanOutEviction{},
	}
}

type rootFanOutMember struct{}

type rootFanOutAllocator struct {
	next id.MemberID
}

func (a *rootFanOutAllocator) Allocate(
	context.Context,
) (id.MemberID, error) {
	allocated := a.next
	a.next++
	return allocated, nil
}

type rootFanOutEviction struct{}

func (rootFanOutEviction) Evict(
	context.Context,
	member.Entry[string, rootFanOutMember],
) (eviction.Result, error) {
	return eviction.Result{}, nil
}
