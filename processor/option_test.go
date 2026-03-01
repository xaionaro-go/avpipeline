package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptionQueueSizeInput_Apply(t *testing.T) {
	cfg := config{}
	opt := OptionQueueSizeInput(42)
	opt.apply(&cfg)
	assert.Equal(t, uint(42), cfg.InputQueue)
}

func TestOptionQueueSizeOutput_Apply(t *testing.T) {
	cfg := config{}
	opt := OptionQueueSizeOutput(99)
	opt.apply(&cfg)
	assert.Equal(t, uint(99), cfg.OutputQueue)
}

func TestOptionQueueSizeError_Apply(t *testing.T) {
	cfg := config{}
	opt := OptionQueueSizeError(7)
	opt.apply(&cfg)
	assert.Equal(t, uint(7), cfg.ErrorQueue)
}

func TestOptions_Apply_Combined(t *testing.T) {
	opts := Options{
		OptionQueueSizeInput(10),
		OptionQueueSizeOutput(20),
		OptionQueueSizeError(30),
	}
	cfg := opts.config()
	assert.Equal(t, uint(10), cfg.InputQueue)
	assert.Equal(t, uint(20), cfg.OutputQueue)
	assert.Equal(t, uint(30), cfg.ErrorQueue)
}

func TestOptions_Apply_LastWins(t *testing.T) {
	opts := Options{
		OptionQueueSizeInput(5),
		OptionQueueSizeInput(10),
	}
	cfg := opts.config()
	assert.Equal(t, uint(10), cfg.InputQueue, "last option should win")
}

func TestOptions_Config_Empty(t *testing.T) {
	opts := Options{}
	cfg := opts.config()
	assert.Equal(t, uint(0), cfg.InputQueue)
	assert.Equal(t, uint(0), cfg.OutputQueue)
	assert.Equal(t, uint(0), cfg.ErrorQueue)
}
