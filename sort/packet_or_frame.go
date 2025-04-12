package sort

import (
	"github.com/xaionaro-go/avpipeline/types"
)

type AbstractPacketOrFrames []types.AbstractPacketOrFrame

func (s AbstractPacketOrFrames) Len() int {
	return len(s)
}

func (s AbstractPacketOrFrames) Less(i, j int) bool {
	return s[i].GetPTS() < s[j].GetPTS()
}

func (s AbstractPacketOrFrames) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type PacketOrFrames[T types.PacketOrFrame, TP types.PacketOrFramePointer[T]] []T

func (s PacketOrFrames[T, TP]) Len() int {
	return len(s)
}

func (s PacketOrFrames[T, TP]) Less(i, j int) bool {
	return (TP)(&s[i]).GetPTS() < (TP)(&s[j]).GetPTS()
}

func (s PacketOrFrames[T, TP]) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type InputPacketOrFrameUnions []types.InputPacketOrFrameUnion

func (s InputPacketOrFrameUnions) Len() int {
	return len(s)
}

func (s InputPacketOrFrameUnions) Less(i, j int) bool {
	return s[i].GetPTS() < s[j].GetPTS()
}

func (s InputPacketOrFrameUnions) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
