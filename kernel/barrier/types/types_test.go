package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{UndefinedState, "<undefined>"},
		{StatePass, "pass"},
		{StateBlock, "block"},
		{StateDrop, "drop"},
		{State(99), "unknown_99"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.state.String())
	}
}

func TestStateFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected State
		hasError bool
	}{
		{"pass", StatePass, false},
		{"block", StateBlock, false},
		{"drop", StateDrop, false},
		{"Pass", StatePass, false},  // case insensitive
		{"BLOCK", StateBlock, false}, // case insensitive
		{"unknown", UndefinedState, true},
		{"", UndefinedState, true},
	}
	for _, tt := range tests {
		result, err := StateFromString(tt.input)
		if tt.hasError {
			assert.Error(t, err, "input: %q", tt.input)
		} else {
			require.NoError(t, err, "input: %q", tt.input)
			assert.Equal(t, tt.expected, result, "input: %q", tt.input)
		}
	}
}

func TestState_JSON_RoundTrip(t *testing.T) {
	for s := UndefinedState; s < EndOfState; s++ {
		data, err := json.Marshal(s)
		require.NoError(t, err)

		var decoded State
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)
		assert.Equal(t, s, decoded, "round trip failed for %v", s)
	}
}

func TestState_JSON_MarshalFormat(t *testing.T) {
	data, err := json.Marshal(StatePass)
	require.NoError(t, err)
	assert.Equal(t, `"pass"`, string(data))
}

func TestState_UnmarshalJSON_InvalidJSON(t *testing.T) {
	var s State
	err := json.Unmarshal([]byte(`123`), &s)
	assert.Error(t, err)
}

func TestState_UnmarshalJSON_UnknownState(t *testing.T) {
	var s State
	err := json.Unmarshal([]byte(`"garbage"`), &s)
	assert.Error(t, err)
}

func TestSwitchFlags_HasAll(t *testing.T) {
	f := SwitchFlagFirstPacketAfterSwitchPassBothOutputs | SwitchFlagForbidTakeoverInKeepUnless
	assert.True(t, f.HasAll(SwitchFlagFirstPacketAfterSwitchPassBothOutputs))
	assert.True(t, f.HasAll(SwitchFlagForbidTakeoverInKeepUnless))
	assert.True(t, f.HasAll(SwitchFlagFirstPacketAfterSwitchPassBothOutputs|SwitchFlagForbidTakeoverInKeepUnless))
	assert.False(t, f.HasAll(SwitchFlagNextOutputStateBlock))
}

func TestSwitchFlags_HasAny(t *testing.T) {
	f := SwitchFlagFirstPacketAfterSwitchPassBothOutputs
	assert.True(t, f.HasAny(SwitchFlagFirstPacketAfterSwitchPassBothOutputs|SwitchFlagForbidTakeoverInKeepUnless))
	assert.False(t, f.HasAny(SwitchFlagForbidTakeoverInKeepUnless))
}

func TestSwitchFlags_Set(t *testing.T) {
	var f SwitchFlags
	f.Set(SwitchFlagFirstPacketAfterSwitchPassBothOutputs)
	assert.True(t, f.HasAll(SwitchFlagFirstPacketAfterSwitchPassBothOutputs))
}

func TestSwitchFlags_Unset(t *testing.T) {
	f := SwitchFlagFirstPacketAfterSwitchPassBothOutputs | SwitchFlagForbidTakeoverInKeepUnless
	f.Unset(SwitchFlagFirstPacketAfterSwitchPassBothOutputs)
	assert.False(t, f.HasAny(SwitchFlagFirstPacketAfterSwitchPassBothOutputs))
	assert.True(t, f.HasAll(SwitchFlagForbidTakeoverInKeepUnless))
}

func TestSwitchFlags_ZeroValue(t *testing.T) {
	var f SwitchFlags
	assert.False(t, f.HasAny(SwitchFlagFirstPacketAfterSwitchPassBothOutputs))
	assert.False(t, f.HasAny(SwitchFlagForbidTakeoverInKeepUnless))
	assert.False(t, f.HasAny(SwitchFlagNextOutputStateBlock))
	assert.False(t, f.HasAny(SwitchFlagInactiveBlock))
}
