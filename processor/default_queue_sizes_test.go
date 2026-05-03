// default_queue_sizes_test.go covers SetDefaultQueueSizes:
// - sentinel-zero leaves a role/channel unchanged;
// - non-zero values flow into DefaultOptionsTranscoder/Output factories;
// - validation rejects values exceeding MaxQueueSize without mutating
//   package state.

package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotDefaults returns a copy of the live default queue sizes; tests
// use it to restore package state regardless of test order.
func snapshotDefaults() DefaultQueueSizes {
	return loadDefaultQueueSizes()
}

// restoreDefaults atomically resets the package-level state to a captured
// snapshot. Used as `defer restoreDefaults(snap)` in every test that calls
// SetDefaultQueueSizes.
func restoreDefaults(s DefaultQueueSizes) {
	defaultQueueSizes.Store(&s)
}

// resetToBuiltin clears the package-level override so subsequent factory
// calls fall back to builtinDefaultQueueSizes.
func resetToBuiltin() {
	defaultQueueSizes.Store(nil)
}

func TestSetDefaultQueueSizes_AppliesAllRoles(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	require.NoError(t, SetDefaultQueueSizes(11, 12, 13, 21, 22, 23))

	tCfg := Options(DefaultOptionsTranscoder()).config()
	assert.Equal(t, uint(11), tCfg.InputQueue)
	assert.Equal(t, uint(12), tCfg.OutputQueue)
	assert.Equal(t, uint(13), tCfg.ErrorQueue)

	oCfg := Options(DefaultOptionsOutput()).config()
	assert.Equal(t, uint(21), oCfg.InputQueue)
	assert.Equal(t, uint(22), oCfg.OutputQueue)
	assert.Equal(t, uint(23), oCfg.ErrorQueue)
}

func TestSetDefaultQueueSizes_SentinelZeroLeavesUnchanged(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	require.NoError(t, SetDefaultQueueSizes(11, 12, 13, 21, 22, 23))
	// Pass 0 for transcoderInput and outputOutput; both must be preserved.
	require.NoError(t, SetDefaultQueueSizes(0, 99, 0, 0, 0, 0))

	tCfg := Options(DefaultOptionsTranscoder()).config()
	assert.Equal(t, uint(11), tCfg.InputQueue, "transcoderInput preserved by sentinel 0")
	assert.Equal(t, uint(99), tCfg.OutputQueue, "transcoderOutput updated to 99")
	assert.Equal(t, uint(13), tCfg.ErrorQueue, "transcoderError preserved by sentinel 0")

	oCfg := Options(DefaultOptionsOutput()).config()
	assert.Equal(t, uint(21), oCfg.InputQueue)
	assert.Equal(t, uint(22), oCfg.OutputQueue)
	assert.Equal(t, uint(23), oCfg.ErrorQueue)
}

func TestSetDefaultQueueSizes_RejectsOverflow(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	// Establish a known baseline, then attempt an invalid update.
	require.NoError(t, SetDefaultQueueSizes(11, 12, 13, 21, 22, 23))

	err := SetDefaultQueueSizes(0, 0, 0, 0, MaxQueueSize+1, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outputOutput")
	assert.Contains(t, err.Error(), "exceeds MaxQueueSize")

	// State must be unchanged on validation failure.
	tCfg := Options(DefaultOptionsTranscoder()).config()
	assert.Equal(t, uint(11), tCfg.InputQueue)
	assert.Equal(t, uint(12), tCfg.OutputQueue)
	assert.Equal(t, uint(13), tCfg.ErrorQueue)

	oCfg := Options(DefaultOptionsOutput()).config()
	assert.Equal(t, uint(21), oCfg.InputQueue)
	assert.Equal(t, uint(22), oCfg.OutputQueue)
	assert.Equal(t, uint(23), oCfg.ErrorQueue)
}

func TestSetDefaultQueueSizes_RejectsAggregatesAllInvalidParameters(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	err := SetDefaultQueueSizes(MaxQueueSize+1, 0, 0, MaxQueueSize+2, 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transcoderInput")
	assert.Contains(t, err.Error(), "outputInput")
}

func TestDefaultOptionsTranscoder_BuiltinWhenUnset(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	cfg := Options(DefaultOptionsTranscoder()).config()
	assert.Equal(t, uint(60), cfg.InputQueue)
	assert.Equal(t, uint(10), cfg.OutputQueue)
	assert.Equal(t, uint(2), cfg.ErrorQueue)
}

func TestDefaultOptionsOutput_BuiltinWhenUnset(t *testing.T) {
	snap := snapshotDefaults()
	defer restoreDefaults(snap)
	resetToBuiltin()

	cfg := Options(DefaultOptionsOutput()).config()
	assert.Equal(t, uint(60), cfg.InputQueue)
	assert.Equal(t, uint(0), cfg.OutputQueue)
	assert.Equal(t, uint(2), cfg.ErrorQueue)
}
