
all: libav avpipeline

libav:
	rm -rf github.com libav
	protoc --go_out=. --go-grpc_out=. libav.proto
	mv github.com/xaionaro-go/avpipeline/protobuf/libav libav
	rm -rf github.com

avpipeline:
	rm -rf github.com avpipeline
	protoc --go_out=. --go-grpc_out=. avpipeline.proto
	mv github.com/xaionaro-go/avpipeline/protobuf/avpipeline avpipeline
	rm -rf github.com
