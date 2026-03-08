package symbolresolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariable_Creation(t *testing.T) {
	val := 42.0
	v := Variable[any, float64]("test_var", &val)
	require.NotNil(t, v)
	assert.Equal(t, "test_var", v.Name)
	assert.Equal(t, &val, v.Pointer)
}

func TestVariableT_Resolve_MatchingName(t *testing.T) {
	val := 3.14
	v := Variable[any, float64]("pi", &val)

	loader, err := v.Resolve("pi")
	require.NoError(t, err)
	require.NotNil(t, loader)

	result := loader.Load(nil)
	assert.Equal(t, 3.14, result)
}

func TestVariableT_Resolve_NonMatchingName(t *testing.T) {
	val := 3.14
	v := Variable[any, float64]("pi", &val)

	loader, err := v.Resolve("not_pi")
	assert.NoError(t, err)
	assert.Nil(t, loader)
}

func TestVariableT_Resolve_UpdatedValue(t *testing.T) {
	val := 1.0
	v := Variable[any, float64]("x", &val)

	loader, err := v.Resolve("x")
	require.NoError(t, err)
	require.NotNil(t, loader)

	assert.Equal(t, 1.0, loader.Load(nil))

	// Update the value and re-read through the same loader
	val = 2.0
	assert.Equal(t, 2.0, loader.Load(nil))
}

func TestVariableT_Resolve_IntType(t *testing.T) {
	val := int64(100)
	v := Variable[string, int64]("count", &val)

	loader, err := v.Resolve("count")
	require.NoError(t, err)
	require.NotNil(t, loader)

	result := loader.Load("any_arg")
	assert.Equal(t, int64(100), result)
}
