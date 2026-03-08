package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRational_Reverse(t *testing.T) {
	r := Rational{Num: 2, Den: 3}
	rev := r.Reverse()
	assert.Equal(t, 3, rev.Num)
	assert.Equal(t, 2, rev.Den)
}

func TestRational_Reverse_Identity(t *testing.T) {
	r := Rational{Num: 1, Den: 1}
	rev := r.Reverse()
	assert.Equal(t, r, rev)
}

func TestRational_Mul(t *testing.T) {
	a := Rational{Num: 2, Den: 3}
	b := Rational{Num: 3, Den: 4}
	result := a.Mul(b)
	assert.Equal(t, 6, result.Num)
	assert.Equal(t, 12, result.Den)
}

func TestRational_Mul_ByZero(t *testing.T) {
	a := Rational{Num: 5, Den: 7}
	b := Rational{Num: 0, Den: 1}
	result := a.Mul(b)
	assert.Equal(t, 0, result.Num)
}

func TestRational_Div(t *testing.T) {
	a := Rational{Num: 2, Den: 3}
	b := Rational{Num: 4, Den: 5}
	result := a.Div(b)
	assert.Equal(t, 10, result.Num)
	assert.Equal(t, 12, result.Den)
}

func TestRational_String(t *testing.T) {
	r := Rational{Num: 24, Den: 1}
	assert.Equal(t, "24/1", r.String())

	r2 := Rational{Num: 24000, Den: 1001}
	assert.Equal(t, "24000/1001", r2.String())
}

func TestRational_Float64(t *testing.T) {
	r := Rational{Num: 1, Den: 2}
	assert.InDelta(t, 0.5, r.Float64(), 1e-9)

	r2 := Rational{Num: 24000, Den: 1001}
	assert.InDelta(t, 23.976, r2.Float64(), 0.001)
}

func TestRational_JSON_RoundTrip(t *testing.T) {
	original := Rational{Num: 24000, Den: 1001}
	data, err := json.Marshal(original)
	require.NoError(t, err)
	assert.Equal(t, `"24000/1001"`, string(data))

	var decoded Rational
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestRational_JSON_Unmarshal_InvalidString(t *testing.T) {
	var r Rational
	err := json.Unmarshal([]byte(`"invalid"`), &r)
	assert.Error(t, err)
}

func TestRational_JSON_Unmarshal_InvalidJSON(t *testing.T) {
	var r Rational
	err := json.Unmarshal([]byte(`123`), &r)
	assert.Error(t, err)
}

func TestRational_YAML_RoundTrip(t *testing.T) {
	original := Rational{Num: 30, Den: 1}
	data, err := original.MarshalYAML()
	require.NoError(t, err)

	var decoded Rational
	err = decoded.UnmarshalYAML(data)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestHardwareDeviceName_YAML_RoundTrip(t *testing.T) {
	name := HardwareDeviceName("cuda")
	data, err := name.MarshalYAML()
	require.NoError(t, err)

	var decoded HardwareDeviceName
	err = decoded.UnmarshalYAML(data)
	require.NoError(t, err)
	assert.Equal(t, name, decoded)
}

func TestHardwareDeviceName_YAML_Empty(t *testing.T) {
	name := HardwareDeviceName("")
	data, err := name.MarshalYAML()
	require.NoError(t, err)

	var decoded HardwareDeviceName
	err = decoded.UnmarshalYAML(data)
	require.NoError(t, err)
	assert.Equal(t, name, decoded)
}
