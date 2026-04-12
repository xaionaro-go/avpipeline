// pool.go implements a pool for reusing astiav.Frame objects.

package frame

import (
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/pool"
)

var Pool = pool.NewPool(
	astiav.AllocFrame,
	func(p *astiav.Frame) { p.Unref() },
	func(p *astiav.Frame) { p.Free() },
)

func CopyReferenced(dst, src *astiav.Frame) {
	dst.Ref(src)
}

func CloneAsReferenced(src *astiav.Frame) *astiav.Frame {
	dst := Pool.Get()
	CopyReferenced(dst, src)
	return dst
}

func CopyWritable(dst, src *astiav.Frame) error {
	dst.Ref(src)
	if err := dst.MakeWritable(); err != nil {
		return fmt.Errorf("unable to make frame writable: %w", err)
	}
	return nil
}

func CloneAsWritable(src *astiav.Frame) (*astiav.Frame, error) {
	dst := Pool.Get()
	if err := CopyWritable(dst, src); err != nil {
		Pool.Put(dst)
		return nil, err
	}
	return dst, nil
}
