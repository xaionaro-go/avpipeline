//go:build with_libav
// +build with_libav

package server

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
	goconvlibav "github.com/xaionaro-go/avpipeline/kernel/server/grpc/common/goconv/libav"
	kernel_grpc "github.com/xaionaro-go/avpipeline/kernel/server/grpc/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
	libav_protobuf "github.com/xaionaro-go/avpipeline/protobuf/libav"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type Server[K kernel.Abstract] struct {
	kernel_grpc.UnimplementedKernelServer
	Kernel K
}

func New[K kernel.Abstract](kernel K) *Server[K] {
	return &Server[K]{
		Kernel: kernel,
	}
}

func (s *Server[K]) SendInputPacket(
	ctx context.Context,
	req *kernel_grpc.SendInputPacketRequest,
) (*kernel_grpc.SendInputPacketReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SendInputPacket not implemented")
}

func (s *Server[K]) SendInputFrame(
	ctx context.Context,
	req *kernel_grpc.SendInputFrameRequest,
) (*kernel_grpc.SendInputFrameReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SendInputFrame not implemented")
}

func (s *Server[K]) String(
	ctx context.Context,
	_ *emptypb.Empty,
) (*wrapperspb.StringValue, error) {
	return &wrapperspb.StringValue{
		Value: s.Kernel.String(),
	}, nil
}

func (s *Server[K]) Close(
	ctx context.Context,
	_ *emptypb.Empty,
) (*emptypb.Empty, error) {
	if err := s.Kernel.Close(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *Server[K]) CloseChan(
	_ *emptypb.Empty,
	srv kernel_grpc.Kernel_CloseChanServer,
) error {
	return status.Errorf(codes.Unimplemented, "method CloseChan not implemented")
}

func (s *Server[K]) GeneratePackets(
	_ *emptypb.Empty,
	srv kernel_grpc.Kernel_GeneratePacketsServer,
) error {
	return status.Errorf(codes.Unimplemented, "method GeneratePackets not implemented")
}

func (s *Server[K]) GenerateFrame(
	_ *emptypb.Empty,
	srv kernel_grpc.Kernel_GenerateFrameServer,
) error {
	return status.Errorf(codes.Unimplemented, "method GenerateFrame not implemented")
}

func (s *Server[K]) GetOutputFormatContext(
	ctx context.Context,
	_ *emptypb.Empty,
) (*kernel_grpc.GetOutputFormatContextReply, error) {
	packetSource, ok := any(s.Kernel).(packet.Source)
	if !ok {
		return &kernel_grpc.GetOutputFormatContextReply{}, nil
	}

	var fmtCtx *libav_protobuf.FormatContext
	packetSource.WithOutputFormatContext(ctx, func(input *astiav.FormatContext) {
		fmtCtx = goconvlibav.FormatContextFromGo(input).Protobuf()
	})
	return &kernel_grpc.GetOutputFormatContextReply{
		FormatContext: fmtCtx,
	}, nil
}

func (s *Server[K]) GetInputFormatContext(
	ctx context.Context,
	_ *emptypb.Empty,
) (*kernel_grpc.GetInputFormatContextReply, error) {
	packetSource, ok := any(s.Kernel).(packet.Sink)
	if !ok {
		return &kernel_grpc.GetInputFormatContextReply{}, nil
	}

	var fmtCtx *libav_protobuf.FormatContext
	packetSource.WithInputFormatContext(ctx, func(input *astiav.FormatContext) {
		fmtCtx = goconvlibav.FormatContextFromGo(input).Protobuf()
	})
	return &kernel_grpc.GetInputFormatContextReply{
		FormatContext: fmtCtx,
	}, nil
}

func (s *Server[K]) NotifyAboutPacketFormatContext(
	ctx context.Context,
	req *kernel_grpc.NotifyAboutPacketFormatContextRequest,
) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NotifyAboutPacketFormatContext not implemented")
}
