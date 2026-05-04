package selector_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector"
	selectorid "github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func TestIDAliasesAreAssignmentCompatible(t *testing.T) {
	var rootMember selector.MemberID = 17
	var leafMember selectorid.MemberID = rootMember
	var rootMemberAgain selector.MemberID = leafMember

	require.Equal(t, selector.MemberID(17), rootMemberAgain)
	require.Equal(t, int32(17), int32(rootMemberAgain))

	var rootRoute selector.RouteID = "primary"
	var leafRoute selectorid.RouteID = rootRoute
	var rootRouteAgain selector.RouteID = leafRoute

	require.Equal(t, selector.RouteID("primary"), rootRouteAgain)
	require.Equal(t, "primary", string(rootRouteAgain))
}

func TestNoMemberIDMatchesBarrierDemotionSentinel(t *testing.T) {
	require.Equal(t, selectorid.NoMemberID, selector.NoMemberID)
	require.Equal(t, selector.MemberID(math.MinInt32), selector.NoMemberID)
}
