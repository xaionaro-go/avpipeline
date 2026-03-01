package node

import (
	"errors"
	"fmt"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/processor"
)

func TestError_Error(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)
	innerErr := fmt.Errorf("something broke")

	nodeErr := Error{
		Node: n,
		Err:  innerErr,
	}

	msg := nodeErr.Error()
	tassert.Contains(t, msg, "Dummy")
	tassert.Contains(t, msg, "something broke")
	tassert.Equal(t, fmt.Sprintf("received an error on %s: %v", proc, innerErr), msg)
}

func TestError_Error_WithDebugData(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)
	innerErr := fmt.Errorf("test error")

	nodeErr := Error{
		Node:      n,
		Err:       innerErr,
		DebugData: "debug-info-123",
	}

	msg := nodeErr.Error()
	tassert.Contains(t, msg, "test error")
	// DebugData is not included in error string, just stored
	tassert.Equal(t, "debug-info-123", nodeErr.DebugData)
}

func TestError_Unwrap(t *testing.T) {
	innerErr := fmt.Errorf("inner error")
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)

	nodeErr := Error{
		Node: n,
		Err:  innerErr,
	}

	unwrapped := nodeErr.Unwrap()
	tassert.Equal(t, innerErr, unwrapped)
}

func TestError_Unwrap_Nil(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)

	nodeErr := Error{
		Node: n,
		Err:  nil,
	}

	unwrapped := nodeErr.Unwrap()
	tassert.Nil(t, unwrapped)
}

func TestError_ErrorsIs(t *testing.T) {
	sentinel := fmt.Errorf("sentinel error")
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)

	nodeErr := Error{
		Node: n,
		Err:  sentinel,
	}

	tassert.True(t, errors.Is(nodeErr, sentinel))
}

func TestError_ErrorsAs(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)

	nodeErr := Error{
		Node: n,
		Err:  fmt.Errorf("test"),
	}
	wrapped := fmt.Errorf("wrapping: %w", nodeErr)

	var target Error
	require.True(t, errors.As(wrapped, &target))
	tassert.Equal(t, nodeErr.Error(), target.Error())
}

func TestErrAlreadyStarted_Error(t *testing.T) {
	err := ErrAlreadyStarted{}
	tassert.Equal(t, "already started serving", err.Error())
}

func TestErrAlreadyStarted_ErrorWithPreviousDebugData(t *testing.T) {
	err := ErrAlreadyStarted{
		PreviousDebugData: "previous-debug-data",
	}
	tassert.Equal(t, "already started serving", err.Error())
	tassert.Equal(t, "previous-debug-data", err.PreviousDebugData)
}

func TestErrAlreadyStarted_ImplementsError(t *testing.T) {
	var err error = ErrAlreadyStarted{}
	tassert.NotNil(t, err)
	tassert.Equal(t, "already started serving", err.Error())
}

func TestError_ImplementsErrorInterface(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)
	var err error = Error{
		Node: n,
		Err:  fmt.Errorf("test"),
	}
	tassert.NotNil(t, err)
}
