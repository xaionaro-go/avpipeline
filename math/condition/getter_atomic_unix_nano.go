package condition

import (
	"context"
	"fmt"
	"time"

	"github.com/go-ng/xatomic"
)

type GetterAtomicUNIXNanoT struct {
	xatomic.Pointer[time.Time]
}

func GetterAtomicUNIXNano(
	ptr xatomic.Pointer[time.Time],
) GetterAtomicUNIXNanoT {
	return GetterAtomicUNIXNanoT{Pointer: ptr}
}

var _ Getter[int64] = GetterAtomicUNIXNanoT{}

func (g GetterAtomicUNIXNanoT) String() string {
	return fmt.Sprintf("AtomicUNIXNano(%v)", g.Get(context.Background()))
}

func (g GetterAtomicUNIXNanoT) Get(context.Context) int64 {
	v := g.Pointer.Load()
	if v == nil {
		return 0
	}
	return v.UnixNano()
}
