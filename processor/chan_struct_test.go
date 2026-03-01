package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChanStruct_ChannelCapacitiesMatchParameters(t *testing.T) {
	cs := NewChanStruct(10, 20, 30)
	require.NotNil(t, cs)

	assert.Equal(t, 10, cap(cs.InputCh), "InputCh capacity should be 10")
	assert.Equal(t, 20, cap(cs.OutputCh), "OutputCh capacity should be 20")
	assert.Equal(t, 30, cap(cs.ErrorCh), "ErrorCh capacity should be 30")
}

func TestNewChanStruct_ZeroCapacityChannels(t *testing.T) {
	cs := NewChanStruct(0, 0, 0)
	require.NotNil(t, cs)

	assert.Equal(t, 0, cap(cs.InputCh), "InputCh capacity should be 0 (unbuffered)")
	assert.Equal(t, 0, cap(cs.OutputCh), "OutputCh capacity should be 0 (unbuffered)")
	assert.Equal(t, 0, cap(cs.ErrorCh), "ErrorCh capacity should be 0 (unbuffered)")
}

func TestNewChanStruct_ChannelsAreNotNil(t *testing.T) {
	cs := NewChanStruct(0, 0, 0)
	require.NotNil(t, cs)

	assert.NotNil(t, cs.InputCh, "InputCh should not be nil even with zero capacity")
	assert.NotNil(t, cs.OutputCh, "OutputCh should not be nil even with zero capacity")
	assert.NotNil(t, cs.ErrorCh, "ErrorCh should not be nil even with zero capacity")
}

func TestNewChanStruct_LargeCapacity(t *testing.T) {
	cs := NewChanStruct(1000, 500, 100)
	require.NotNil(t, cs)

	assert.Equal(t, 1000, cap(cs.InputCh))
	assert.Equal(t, 500, cap(cs.OutputCh))
	assert.Equal(t, 100, cap(cs.ErrorCh))
}

func TestNewChanStruct_AsymmetricCapacities(t *testing.T) {
	cs := NewChanStruct(1, 0, 5)
	require.NotNil(t, cs)

	assert.Equal(t, 1, cap(cs.InputCh))
	assert.Equal(t, 0, cap(cs.OutputCh))
	assert.Equal(t, 5, cap(cs.ErrorCh))
}

func TestNewChanStruct_ChannelLengthsInitiallyZero(t *testing.T) {
	cs := NewChanStruct(10, 10, 10)
	require.NotNil(t, cs)

	assert.Equal(t, 0, len(cs.InputCh), "InputCh should be initially empty")
	assert.Equal(t, 0, len(cs.OutputCh), "OutputCh should be initially empty")
	assert.Equal(t, 0, len(cs.ErrorCh), "ErrorCh should be initially empty")
}
