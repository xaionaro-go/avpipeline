package router

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublishMode_String(t *testing.T) {
	tests := []struct {
		mode     PublishMode
		expected string
	}{
		{UndefinedPublishMode, "<undefined>"},
		{PublishModeExclusiveTakeover, "exclusive-takeover"},
		{PublishModeExclusiveFail, "exclusive-fail"},
		{PublishModeSharedTakeover, "shared-takeover"},
		{PublishModeSharedFail, "shared-fail"},
		{PublishMode(999), "<unknown_mode_999>"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.String())
		})
	}
}

func TestPublishMode_IsExclusive(t *testing.T) {
	tests := []struct {
		mode     PublishMode
		expected bool
	}{
		{UndefinedPublishMode, false},
		{PublishModeExclusiveTakeover, true},
		{PublishModeExclusiveFail, true},
		{PublishModeSharedTakeover, false},
		{PublishModeSharedFail, false},
		{PublishMode(999), false},
	}
	for _, tc := range tests {
		t.Run(tc.mode.String(), func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.IsExclusive())
		})
	}
}

func TestPublishMode_FailOnConflict(t *testing.T) {
	tests := []struct {
		mode     PublishMode
		expected bool
	}{
		{UndefinedPublishMode, false},
		{PublishModeExclusiveTakeover, false},
		{PublishModeExclusiveFail, true},
		{PublishModeSharedTakeover, false},
		{PublishModeSharedFail, true},
		{PublishMode(999), false},
	}
	for _, tc := range tests {
		t.Run(tc.mode.String(), func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.mode.FailOnConflict())
		})
	}
}

func TestPublishMode_Constants_AreSequential(t *testing.T) {
	assert.Equal(t, PublishMode(0), UndefinedPublishMode)
	assert.Equal(t, PublishMode(1), PublishModeExclusiveTakeover)
	assert.Equal(t, PublishMode(2), PublishModeExclusiveFail)
	assert.Equal(t, PublishMode(3), PublishModeSharedTakeover)
	assert.Equal(t, PublishMode(4), PublishModeSharedFail)
	assert.Equal(t, PublishMode(5), EndOfPublishMode)
}

func TestPublishMode_AllDefinedModes_HaveStringRepresentation(t *testing.T) {
	for m := UndefinedPublishMode; m < EndOfPublishMode; m++ {
		s := m.String()
		assert.NotContains(t, s, "unknown_mode", "mode %d should have a defined string", int(m))
	}
}

func TestPublishMode_Stringer_Interface(t *testing.T) {
	var m PublishMode = PublishModeExclusiveTakeover
	s := fmt.Sprintf("%s", m)
	assert.Equal(t, "exclusive-takeover", s)
}
