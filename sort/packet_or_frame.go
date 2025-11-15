package sort

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type AbstractPacketOrFrames []packetorframe.Abstract

func (s AbstractPacketOrFrames) Len() int {
	return len(s)
}

func (s AbstractPacketOrFrames) Less(i, j int) bool {
	return s[i].GetPTS() < s[j].GetPTS()
}

func (s AbstractPacketOrFrames) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type PacketOrFrames[T packetorframe.Any, TP packetorframe.Pointer[T]] []T

func (s PacketOrFrames[T, TP]) Len() int {
	return len(s)
}

func (s PacketOrFrames[T, TP]) Less(i, j int) bool {
	return (TP)(&s[i]).GetPTS() < (TP)(&s[j]).GetPTS()
}

func (s PacketOrFrames[T, TP]) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type InputPacketOrFrameUnionsByPTS []packetorframe.InputUnion

func (s InputPacketOrFrameUnionsByPTS) Len() int {
	return len(s)
}

func (s InputPacketOrFrameUnionsByPTS) Less(i, j int) bool {
	return s[i].GetPTS() < s[j].GetPTS()
}

func (s InputPacketOrFrameUnionsByPTS) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type InputPacketOrFrameUnionsByDTS []packetorframe.InputUnion

func (s InputPacketOrFrameUnionsByDTS) Len() int {
	return len(s)
}

func (s InputPacketOrFrameUnionsByDTS) Less(i, j int) bool {
	return s[i].GetDTS() < s[j].GetDTS()
}

func (s InputPacketOrFrameUnionsByDTS) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
