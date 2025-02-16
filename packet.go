package avpipeline

import (
	"github.com/asticode/go-astiav"
)

var PacketPool = newPool(
	astiav.AllocPacket,
	func(p *astiav.Packet) { p.Unref() },
	func(p *astiav.Packet) { p.Free() },
)

func CopyPacketReferenced(dst, src *astiav.Packet) {
	dst.Ref(src)
}

func ClonePacketAsReferenced(src *astiav.Packet) *astiav.Packet {
	dst := PacketPool.Get()
	CopyPacketReferenced(dst, src)
	return dst
}

func CopyPacketWritable(dst, src *astiav.Packet) {
	dst.Ref(src)
	err := dst.MakeWritable()
	if err != nil {
		panic(err)
	}
}

func ClonePacketAsWritable(src *astiav.Packet) *astiav.Packet {
	dst := PacketPool.Get()
	CopyPacketWritable(dst, src)
	return dst
}
