package whisper

import (
	"testing"
	"time"

	testifyassert "github.com/stretchr/testify/assert"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestWhisperResult_PipelineSideData_Roundtrip(t *testing.T) {
	result := &WhisperResult{
		Text:     "hello world",
		Duration: 1500 * time.Millisecond,
	}

	sideData := globaltypes.PipelineSideData{result}
	got, ok := globaltypes.PipelineSideDataLatest[*WhisperResult](sideData)
	testifyassert.True(t, ok)
	testifyassert.Equal(t, "hello world", got.Text)
	testifyassert.Equal(t, 1500*time.Millisecond, got.Duration)
}

func TestWhisperResult_PipelineSideData_NotFound(t *testing.T) {
	sideData := globaltypes.PipelineSideData{"some other data"}
	got, ok := globaltypes.PipelineSideDataLatest[*WhisperResult](sideData)
	testifyassert.False(t, ok)
	testifyassert.Nil(t, got)
}

func TestWhisperResult_PipelineSideData_Empty(t *testing.T) {
	var sideData globaltypes.PipelineSideData
	got, ok := globaltypes.PipelineSideDataLatest[*WhisperResult](sideData)
	testifyassert.False(t, ok)
	testifyassert.Nil(t, got)
}

func TestWhisperResult_PipelineSideData_LatestWins(t *testing.T) {
	first := &WhisperResult{Text: "first"}
	second := &WhisperResult{Text: "second"}
	sideData := globaltypes.PipelineSideData{first, second}

	got, ok := globaltypes.PipelineSideDataLatest[*WhisperResult](sideData)
	testifyassert.True(t, ok)
	testifyassert.Equal(t, "second", got.Text)
}
