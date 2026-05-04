package selectorerr_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func TestSentinelsAreOwnedBySelectorErr(t *testing.T) {
	require.EqualError(t, selectorerr.ErrInvalidConfig, "invalid selector config")
	require.EqualError(t, selectorerr.ErrMemberNotFound, "selector member not found")
	require.EqualError(t, selectorerr.ErrRouteNotFound, "selector route not found")
	require.EqualError(t, selectorerr.ErrAlreadyPreferred, "selector already preferred")
	require.EqualError(t, selectorerr.ErrInvalidRoutePlan, "invalid selector route plan")
}

func TestHelpersWrapSentinelsAndContext(t *testing.T) {
	inner := errors.New("route plan points to unknown member")

	testCases := []struct {
		name     string
		err      error
		sentinel error
		contains []string
	}{
		{
			name:     "invalid config",
			err:      selectorerr.InvalidConfig("CreateMember", inner),
			sentinel: selectorerr.ErrInvalidConfig,
			contains: []string{"CreateMember"},
		},
		{
			name:     "member not found",
			err:      selectorerr.MemberNotFound(id.MemberID(42)),
			sentinel: selectorerr.ErrMemberNotFound,
			contains: []string{"42"},
		},
		{
			name:     "route not found",
			err:      selectorerr.RouteNotFound(id.RouteID("video")),
			sentinel: selectorerr.ErrRouteNotFound,
			contains: []string{"video"},
		},
		{
			name:     "already preferred",
			err:      selectorerr.AlreadyPreferred(id.RouteID("audio"), id.MemberID(3)),
			sentinel: selectorerr.ErrAlreadyPreferred,
			contains: []string{"audio", "3"},
		},
		{
			name:     "invalid route plan",
			err:      selectorerr.InvalidRoutePlan(id.RouteID("all"), inner),
			sentinel: selectorerr.ErrInvalidRoutePlan,
			contains: []string{"all"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.ErrorIs(t, testCase.err, testCase.sentinel)

			for _, expected := range testCase.contains {
				require.Contains(t, testCase.err.Error(), expected)
			}
		})
	}

	require.ErrorIs(t, selectorerr.InvalidConfig("CreateMember", inner), inner)
	require.ErrorIs(t, selectorerr.InvalidRoutePlan(id.RouteID("all"), inner), inner)
}
