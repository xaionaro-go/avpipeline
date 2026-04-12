// pool.go implements a pool for reusing astiav.Packet objects.

package packet

import (
	"fmt"
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

func CopyWritable(dst, src *astiav.Packet) error {
	dst.Ref(src)
	runtime.KeepAlive(src)
	if err := dst.MakeWritable(); err != nil {
		return fmt.Errorf("unable to make packet writable: %w", err)
	}
	return nil
}

func CloneAsWritable(src *astiav.Packet) (*astiav.Packet, error) {
	dst := Pool.Get()
	if err := CopyWritable(dst, src); err != nil {
		Pool.Put(dst)
		return nil, err
	}
	return dst, nil
}
