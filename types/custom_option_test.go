package types

import (
	"testing"

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
