// pool.go implements a pool for reusing astiav.Packet objects.

package packet

import (
	"runtime"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/pool"
)

var Pool = pool.NewPool(
	astiav.AllocPacket,
	func(p *astiav.Packet) { p.Unref() },
	func(p *astiav.Packet) { p.Free() },
)

func CopyReferenced(dst, src *astiav.Packet) {
	dst.Ref(src)
	runtime.KeepAlive(src)
}

func CloneAsReferenced(src *astiav.Packet) *astiav.Packet {
	dst := Pool.Get()
	CopyReferenced(dst, src)
	return dst
}

func CopyWritable(dst, src *astiav.Packet) {
	dst.Ref(src)
	runtime.KeepAlive(src)
	err := dst.MakeWritable()
	if err != nil {
		panic(err)
	}
}

func CloneAsWritable(src *astiav.Packet) *astiav.Packet {
	dst := Pool.Get()
	CopyWritable(dst, src)
	return dst
}
