package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomOptionsDeduplicate(t *testing.T) {
	require.Equal(
		t,
		DictionaryItems{
			{Key: "b", Value: "0"},
			{Key: "a", Value: "1"},
		},
		DictionaryItems{
			{Key: "a", Value: "0"},
			{Key: "b", Value: "0"},
			{Key: "a", Value: "1"},
		}.Deduplicate(),
	)
}

func TestDeduplicate_Nil(t *testing.T) {
	var items DictionaryItems
	assert.Nil(t, items.Deduplicate())
}

func TestDeduplicate_Empty(t *testing.T) {
	items := DictionaryItems{}
	result := items.Deduplicate()
	assert.Len(t, result, 0)
}

func TestDeduplicate_NoDuplicates(t *testing.T) {
	items := DictionaryItems{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
	}
	result := items.Deduplicate()
	assert.Equal(t, items, result)
}

func TestDeduplicate_SingleItem(t *testing.T) {
	items := DictionaryItems{{Key: "a", Value: "1"}}
	result := items.Deduplicate()
	assert.Equal(t, items, result)
}

func TestDeduplicate_KeepsLastValue(t *testing.T) {
	items := DictionaryItems{
		{Key: "codec", Value: "h264"},
		{Key: "bitrate", Value: "2000k"},
		{Key: "codec", Value: "h265"},
	}
	result := items.Deduplicate()
	assert.Len(t, result, 2)
	// Last value for "codec" should be kept
	v := result.GetFirst("codec")
	require.NotNil(t, v)
	assert.Equal(t, "h265", *v)
}

func TestSetFirst_NewKey(t *testing.T) {
	items := DictionaryItems{
		{Key: "a", Value: "1"},
	}
	items.SetFirst(DictionaryItem{Key: "b", Value: "2"})
	assert.Len(t, items, 2)
	v := items.GetFirst("b")
	require.NotNil(t, v)
	assert.Equal(t, "2", *v)
}

func TestSetFirst_ExistingKey(t *testing.T) {
	items := DictionaryItems{
		{Key: "codec", Value: "h264"},
		{Key: "bitrate", Value: "2000k"},
	}
	items.SetFirst(DictionaryItem{Key: "codec", Value: "h265"})
	assert.Len(t, items, 2) // should not grow
	v := items.GetFirst("codec")
	require.NotNil(t, v)
	assert.Equal(t, "h265", *v)
}

func TestSetFirst_OnEmpty(t *testing.T) {
	var items DictionaryItems
	items.SetFirst(DictionaryItem{Key: "key", Value: "value"})
	assert.Len(t, items, 1)
}

func TestGetFirst_Found(t *testing.T) {
	items := DictionaryItems{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
		{Key: "a", Value: "3"}, // duplicate
	}
	v := items.GetFirst("a")
	require.NotNil(t, v)
	assert.Equal(t, "1", *v) // returns first occurrence
}

func TestGetFirst_NotFound(t *testing.T) {
	items := DictionaryItems{
		{Key: "a", Value: "1"},
	}
	assert.Nil(t, items.GetFirst("nonexistent"))
}

func TestGetFirst_Empty(t *testing.T) {
	var items DictionaryItems
	assert.Nil(t, items.GetFirst("any"))
}

// ffstream uses DictionaryItems for custom codec options (codec_hwaccel, force_start_pts, etc.)
// avd uses it for protocol-specific listen config options
func TestDictionaryItems_RealWorldUsage(t *testing.T) {
	// Simulate ffstream custom options flow
	items := DictionaryItems{
		{Key: "codec_hwaccel", Value: "cuda"},
		{Key: "force_start_pts", Value: "0"},
		{Key: "force_start_dts", Value: "0"},
	}

	// Override codec_hwaccel
	items.SetFirst(DictionaryItem{Key: "codec_hwaccel", Value: "vaapi"})
	v := items.GetFirst("codec_hwaccel")
	require.NotNil(t, v)
	assert.Equal(t, "vaapi", *v)

	// Deduplicate preserves the modified version
	deduped := items.Deduplicate()
	v = deduped.GetFirst("codec_hwaccel")
	require.NotNil(t, v)
	assert.Equal(t, "vaapi", *v)
}
