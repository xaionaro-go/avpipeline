package processor

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrNotImplemented_Error(t *testing.T) {
	inner := fmt.Errorf("some method")
	err := ErrNotImplemented{Err: inner}
	assert.Equal(t, "method not implemented: some method", err.Error())
}

func TestErrNotImplemented_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := ErrNotImplemented{Err: inner}

	unwrapped := err.Unwrap()
	assert.Equal(t, inner, unwrapped)
}

func TestErrNotImplemented_ErrorsIs(t *testing.T) {
	sentinel := fmt.Errorf("sentinel")
	err := ErrNotImplemented{Err: sentinel}

	assert.True(t, errors.Is(err, sentinel))
}

func TestErrNotImplemented_ErrorsAs(t *testing.T) {
	err := ErrNotImplemented{Err: fmt.Errorf("test")}
	wrapped := fmt.Errorf("wrapping: %w", err)

	var target ErrNotImplemented
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, "method not implemented: test", target.Error())
}
