
all: kernel

kernel:
	rm -rf github.com kernel
	protoc --go_out=. --go-grpc_out=. -I. -I./common/ kernel.proto
	mv github.com/xaionaro-go/avpipeline/kernel/server/grpc/kernel kernel
	rm -rf github.com