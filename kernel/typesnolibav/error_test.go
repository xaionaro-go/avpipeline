package typesnolibav

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrNotImplemented(t *testing.T) {
	err := ErrNotImplemented{}
	assert.Equal(t, "not implemented", err.Error())
	assert.Implements(t, (*error)(nil), err)
}

func TestErrUnexpectedInputType(t *testing.T) {
	err := ErrUnexpectedInputType{}
	assert.Equal(t, "unexpected input type", err.Error())
	assert.Implements(t, (*error)(nil), err)
}
