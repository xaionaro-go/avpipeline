// atomic_bool.go implements a condition based on an atomic boolean value.

package condition

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/packet"
)

type AtomicBoolT struct {
	Pointer *atomic.Bool
}

var _ Condition = (*AtomicBoolT)(nil)

func AtomicBool(pointer *atomic.Bool) *AtomicBoolT {
	return &AtomicBoolT{Pointer: pointer}
}

func (v *AtomicBoolT) String() string {
	return fmt.Sprintf("AtomicBool(%v)", v.Pointer.Load())
}

func (v *AtomicBoolT) Match(context.Context, packet.Input) bool {
	return v.Pointer.Load()
}
