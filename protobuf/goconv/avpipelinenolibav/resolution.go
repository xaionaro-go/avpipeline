package avpipelinenolibav

import (
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func ResolutionFromProto(r *avpipelinegrpc.Resolution) *codectypes.Resolution {
	if r == nil {
		return nil
	}
	return &codectypes.Resolution{
		Width:  r.GetWidth(),
		Height: r.GetHeight(),
	}
}

func ResolutionToProto(r codectypes.Resolution) *avpipelinegrpc.Resolution {
	return &avpipelinegrpc.Resolution{
		Width:  r.Width,
		Height: r.Height,
	}
}
