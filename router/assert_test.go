package router

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMust_Success(t *testing.T) {
	result := must(42, nil)
	assert.Equal(t, 42, result)
}

func TestMust_Panics(t *testing.T) {
	assert.Panics(t, func() {
		must(0, fmt.Errorf("test error"))
	})
}

func TestMust_WithStringType(t *testing.T) {
	result := must("hello", nil)
	assert.Equal(t, "hello", result)
}
