// pool.go implements a pool for reusing astiav.Frame objects.

package frame

import (
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

func CopyWritable(dst, src *astiav.Frame) {
	dst.Ref(src)
	err := dst.MakeWritable()
	if err != nil {
		panic(err)
	}
}

func CloneAsWritable(src *astiav.Frame) *astiav.Frame {
	dst := Pool.Get()
	CopyWritable(dst, src)
	return dst
}
