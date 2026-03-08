package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestType_String(t *testing.T) {
	assert.Equal(t, "<undefined>", UndefinedType.String())
	assert.Equal(t, "decoder", TypeDecoder.String())
	assert.Equal(t, "encoder", TypeEncoder.String())
	assert.Contains(t, Type(99).String(), "<unexpected_99>")
}

func TestType_Constants(t *testing.T) {
	assert.Equal(t, Type(0), UndefinedType)
	assert.Equal(t, Type(1), TypeDecoder)
	assert.Equal(t, Type(2), TypeEncoder)
	assert.Equal(t, Type(3), EndOfType)
}
