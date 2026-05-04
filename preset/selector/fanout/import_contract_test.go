package fanout_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFanoutPackageDoesNotImportConcreteMediaOrAdapterPackages(t *testing.T) {
	importPrefix := "\"github.com/xaionaro-go/avpipeline"
	forbidden := []string{
		importPrefix + "/preset/streammux",
		importPrefix + "/preset/inputwithfallback",
		importPrefix + "/codec",
		importPrefix + "/kernel/avfilter",
		importPrefix + "/kernel/decoder",
		importPrefix + "/kernel/transcoder",
		importPrefix + "/sender",
		importPrefix + "/factory",
		importPrefix + "/media",
	}

	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, forbiddenImport := range forbidden {
			require.NotContains(t, string(content), forbiddenImport, path)
		}

		return nil
	})
	require.NoError(t, err)
}
