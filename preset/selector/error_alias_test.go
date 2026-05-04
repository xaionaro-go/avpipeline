package selector_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func TestRootErrorAliasesMatchSelectorErrSentinels(t *testing.T) {
	require.Equal(t, selectorerr.ErrInvalidConfig, selector.ErrInvalidConfig)
	require.Equal(t, selectorerr.ErrMemberNotFound, selector.ErrMemberNotFound)
	require.Equal(t, selectorerr.ErrRouteNotFound, selector.ErrRouteNotFound)
	require.Equal(t, selectorerr.ErrAlreadyPreferred, selector.ErrAlreadyPreferred)
	require.Equal(t, selectorerr.ErrInvalidRoutePlan, selector.ErrInvalidRoutePlan)
}

func TestRootErrorAliasesWorkThroughErrorsIs(t *testing.T) {
	inner := errors.New("missing required hook")

	require.ErrorIs(t, selectorerr.InvalidConfig("hook", inner), selector.ErrInvalidConfig)
	require.ErrorIs(t, selectorerr.InvalidConfig("hook", inner), inner)
	require.ErrorIs(t, selectorerr.MemberNotFound(selector.MemberID(7)), selector.ErrMemberNotFound)
	require.ErrorIs(t, selectorerr.RouteNotFound(selector.RouteID("video")), selector.ErrRouteNotFound)
	require.ErrorIs(t, selectorerr.AlreadyPreferred(selector.RouteID("audio"), selector.MemberID(3)), selector.ErrAlreadyPreferred)
	require.ErrorIs(t, selectorerr.InvalidRoutePlan(selector.RouteID("all"), inner), selector.ErrInvalidRoutePlan)
	require.ErrorIs(t, selectorerr.InvalidRoutePlan(selector.RouteID("all"), inner), inner)
}

func TestLeafPackagesDoNotDependOnRootSelectorPackage(t *testing.T) {
	leafPackages := []string{
		"github.com/xaionaro-go/avpipeline/preset/selector/id",
		"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr",
		"github.com/xaionaro-go/avpipeline/preset/selector/safekey",
	}

	for _, leafPackage := range leafPackages {
		t.Run(leafPackage, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-f", "{{join .Deps \"\\n\"}}", leafPackage)

			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			require.NotContains(
				t,
				"\n"+strings.TrimSpace(string(output))+"\n",
				"\ngithub.com/xaionaro-go/avpipeline/preset/selector\n",
			)
		})
	}
}
