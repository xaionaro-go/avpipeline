package router

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrAlreadyClosed_Error(t *testing.T) {
	err := ErrAlreadyClosed{}
	assert.Equal(t, "is already closed", err.Error())
}

func TestErrAlreadyClosed_ImplementsError(t *testing.T) {
	var err error = ErrAlreadyClosed{}
	assert.NotNil(t, err)
	assert.Equal(t, "is already closed", err.Error())
}

func TestErrAlreadyClosed_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyClosed{})
	assert.True(t, errors.Is(err, ErrAlreadyClosed{}))
}

func TestErrAlreadyClosed_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyClosed{})
	var target ErrAlreadyClosed
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "is already closed", target.Error())
}

func TestErrAlreadyOpen_Error(t *testing.T) {
	err := ErrAlreadyOpen{}
	assert.Equal(t, "is already open", err.Error())
}

func TestErrAlreadyOpen_ImplementsError(t *testing.T) {
	var err error = ErrAlreadyOpen{}
	assert.NotNil(t, err)
	assert.Equal(t, "is already open", err.Error())
}

func TestErrAlreadyOpen_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyOpen{})
	assert.True(t, errors.Is(err, ErrAlreadyOpen{}))
}

func TestErrAlreadyOpen_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyOpen{})
	var target ErrAlreadyOpen
	require.True(t, errors.As(err, &target))
}

func TestErrRouteClosed_Error(t *testing.T) {
	err := ErrRouteClosed{}
	assert.Equal(t, "the route is closed", err.Error())
}

func TestErrRouteClosed_ImplementsError(t *testing.T) {
	var err error = ErrRouteClosed{}
	assert.NotNil(t, err)
}

func TestErrRouteClosed_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrRouteClosed{})
	assert.True(t, errors.Is(err, ErrRouteClosed{}))
}

func TestErrRouteClosed_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrRouteClosed{})
	var target ErrRouteClosed
	require.True(t, errors.As(err, &target))
}

func TestErrAlreadyHasPublisher_Error(t *testing.T) {
	err := ErrAlreadyHasPublisher{}
	assert.Equal(t, "is already has a publisher", err.Error())
}

func TestErrAlreadyHasPublisher_ImplementsError(t *testing.T) {
	var err error = ErrAlreadyHasPublisher{}
	assert.NotNil(t, err)
}

func TestErrAlreadyHasPublisher_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyHasPublisher{})
	assert.True(t, errors.Is(err, ErrAlreadyHasPublisher{}))
}

func TestErrAlreadyHasPublisher_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyHasPublisher{})
	var target ErrAlreadyHasPublisher
	require.True(t, errors.As(err, &target))
}

func TestErrAlreadyAPublisher_Error(t *testing.T) {
	err := ErrAlreadyAPublisher{}
	assert.Equal(t, "is already a publisher", err.Error())
}

func TestErrAlreadyAPublisher_ImplementsError(t *testing.T) {
	var err error = ErrAlreadyAPublisher{}
	assert.NotNil(t, err)
}

func TestErrAlreadyAPublisher_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyAPublisher{})
	assert.True(t, errors.Is(err, ErrAlreadyAPublisher{}))
}

func TestErrAlreadyAPublisher_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrAlreadyAPublisher{})
	var target ErrAlreadyAPublisher
	require.True(t, errors.As(err, &target))
}

func TestErrPublisherNotFound_Error(t *testing.T) {
	err := ErrPublisherNotFound{}
	assert.Equal(t, "publisher not found", err.Error())
}

func TestErrPublisherNotFound_ImplementsError(t *testing.T) {
	var err error = ErrPublisherNotFound{}
	assert.NotNil(t, err)
}

func TestErrPublisherNotFound_ErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrPublisherNotFound{})
	assert.True(t, errors.Is(err, ErrPublisherNotFound{}))
}

func TestErrPublisherNotFound_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ErrPublisherNotFound{})
	var target ErrPublisherNotFound
	require.True(t, errors.As(err, &target))
}

func TestAllErrors_AreDistinct(t *testing.T) {
	errs := []error{
		ErrAlreadyClosed{},
		ErrAlreadyOpen{},
		ErrRouteClosed{},
		ErrAlreadyHasPublisher{},
		ErrAlreadyAPublisher{},
		ErrPublisherNotFound{},
		ErrAlreadyAConsumer{},
		ErrConsumerNotFound{},
	}

	messages := make(map[string]bool)
	for _, err := range errs {
		msg := err.Error()
		assert.False(t, messages[msg], "duplicate error message: %s", msg)
		messages[msg] = true
	}
}

func TestAllErrors_HaveNonEmptyMessages(t *testing.T) {
	errs := []error{
		ErrAlreadyClosed{},
		ErrAlreadyOpen{},
		ErrRouteClosed{},
		ErrAlreadyHasPublisher{},
		ErrAlreadyAPublisher{},
		ErrPublisherNotFound{},
		ErrAlreadyAConsumer{},
		ErrConsumerNotFound{},
	}
	for _, err := range errs {
		assert.NotEmpty(t, err.Error(), "error should have a non-empty message")
	}
}
