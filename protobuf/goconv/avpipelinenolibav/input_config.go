package avpipelinenolibav

import (
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func InputConfigFromProto(
	in *avpipelinegrpc.InputConfig,
) kerneltypes.InputConfig {
	if in == nil {
		return kerneltypes.InputConfig{}
	}

	return kerneltypes.InputConfig{
		CustomOptions:      customOptionsFromProto(in.GetCustomOptions()),
		RecvBufferSize:     uint(in.GetRecvBufferSize()),
		AsyncOpen:          in.GetAsyncOpen(),
		AutoClose:          in.GetAutoClose(),
		IgnoreIncorrectDTS: in.GetIgnoreIncorrectDts(),
		IgnoreZeroDuration: in.GetIgnoreZeroDuration(),
		ForceRealTime:      in.ForceRealTime,
		ForceStartPTS:      in.ForceStartPts,
		ForceStartDTS:      in.ForceStartDts,
	}
}

func InputConfigToProto(
	cfg kerneltypes.InputConfig,
) *avpipelinegrpc.InputConfig {
	return &avpipelinegrpc.InputConfig{
		CustomOptions:      customOptionsToProto(cfg.CustomOptions),
		RecvBufferSize:     uint32(cfg.RecvBufferSize),
		AsyncOpen:          cfg.AsyncOpen,
		AutoClose:          cfg.AutoClose,
		IgnoreIncorrectDts: cfg.IgnoreIncorrectDTS,
		IgnoreZeroDuration: cfg.IgnoreZeroDuration,
		ForceRealTime:      cfg.ForceRealTime,
		ForceStartPts:      cfg.ForceStartPTS,
		ForceStartDts:      cfg.ForceStartDTS,
	}
}

func customOptionsFromProto(
	opts []*avpipelinegrpc.CustomOption,
) globaltypes.DictionaryItems {
	if opts == nil {
		return nil
	}
	result := make(globaltypes.DictionaryItems, 0, len(opts))
	for _, opt := range opts {
		result = append(result, globaltypes.DictionaryItem{
			Key:   opt.GetKey(),
			Value: opt.GetValue(),
		})
	}
	return result
}

func customOptionsToProto(
	items globaltypes.DictionaryItems,
) []*avpipelinegrpc.CustomOption {
	if items == nil {
		return nil
	}
	result := make([]*avpipelinegrpc.CustomOption, 0, len(items))
	for _, item := range items {
		result = append(result, &avpipelinegrpc.CustomOption{
			Key:   item.Key,
			Value: item.Value,
		})
	}
	return result
}
