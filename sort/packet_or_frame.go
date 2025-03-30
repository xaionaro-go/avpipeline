package sort

import (
	"github.com/xaionaro-go/avpipeline/types"
)

type InputPacketOrFrames []types.InputPacketOrFrame

func (s InputPacketOrFrames) Len() int {
	return len(s)
}

func (s InputPacketOrFrames) Less(i, j int) bool {
	return s[i].GetPTS() < s[j].GetPTS()
}

func (s InputPacketOrFrames) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
