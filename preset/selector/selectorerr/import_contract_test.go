package selectorerr_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectorErrDoesNotImportRootSelectorPackage(t *testing.T) {
	cmd := exec.Command(
		"go",
		"list",
		"-f",
		"{{join .Imports \"\\n\"}}",
		"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr",
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NotContains(
		t,
		"\n"+strings.TrimSpace(string(output))+"\n",
		"\ngithub.com/xaionaro-go/avpipeline/preset/selector\n",
	)
}
