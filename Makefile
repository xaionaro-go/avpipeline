
all: streamforward-linux-amd64 streamforward-linux-arm64 streamforward-android-arm64

build:
	mkdir -p build

streamforward-linux-amd64: build
	GOOS=linux GOARCH=amd64 go build -o build/streamforward-linux-amd64 ./cmd/streamforward

streamforward-linux-arm64: build
	GOOS=linux GOARCH=arm64 go build -o build/streamforward-linux-arm64 ./cmd/streamforward

streamforward-android-arm64: build dockerbuild-streamforward-android-arm64

DOCKER_IMAGE?=xaionaro2/streamforward-android-builder
DOCKER_CONTAINER_NAME?=streamforward-android-builder

dockerbuilder-android-arm64:
	docker pull  $(DOCKER_IMAGE)
	docker start $(DOCKER_IMAGE) >/dev/null 2>&1 || \
		docker run \
			--detach \
			--init \
			--name $(DOCKER_CONTAINER_NAME) \
			--volume ".:/project" \
			--tty \
			$(DOCKER_IMAGE) >/dev/null 2>&1 || /bin/true

dockerbuild-streamforward-android-arm64: dockerbuilder-android-arm64
	docker exec $(DOCKER_CONTAINER_NAME) make ENABLE_VLC="$(ENABLE_VLC)" ENABLE_LIBAV="$(ENABLE_LIBAV)" FORCE_DEBUG="$(FORCE_DEBUG)" -C /project internal-indocker-streamforward-android-arm64

internal-indocker-streamforward-android-arm64: builddir $(GOPATH)/bin/pkg-config-wrapper
	go mod tidy
	git config --global --add safe.directory /project
	$(eval ANDROID_NDK_HOME=$(shell ls -d /home/builder/lib/android-ndk-* | tail -1))
	cd cmd/streamforward && \
		PKG_CONFIG_WRAPPER_LOG='/tmp/pkg_config_wrapper.log' \
		PKG_CONFIG_WRAPPER_LOG_LEVEL='trace' \
		PKG_CONFIG_LIBS_FORCE_STATIC='libav*,libvlc,libsrt' \
		PKG_CONFIG_ERASE="-fopenmp=*,-landroid,-lcamera2ndk,-lmediandk" \
		PKG_CONFIG='$(GOPATH)/bin/pkg-config-wrapper' \
		PKG_CONFIG_PATH='/data/data/com.termux/files/usr/lib/pkgconfig' \
		CGO_CFLAGS='-I$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/include/ -I/data/data/com.termux/files/usr/include -Wno-incompatible-function-pointer-types -Wno-unused-result -Wno-xor-used-as-pow' \
		CGO_LDFLAGS='-v -Wl,-Bdynamic -ldl -lc -lcamera2ndk -lmediandk -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/ -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/aarch64-linux-android/24/ -L/data/data/com.termux/files/usr/lib' \
		ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" \
		PATH="${PATH}:${HOME}/go/bin" \
		GOFLAGS="$(GOBUILD_FLAGS) -ldflags=$(shell echo ${LINKER_FLAGS_ANDROID} | tr " " ",")" \
		fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streamforward.apk ../../build/streamforward-arm64.apk

$(GOPATH)/bin/pkg-config-wrapper:
	go install github.com/xaionaro-go/pkg-config-wrapper@5dd443e6c18336416c49047e2ba0002e26a85278
