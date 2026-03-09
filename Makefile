
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
		CGO_LDFLAGS='-v -Wl,-Bdynamic -ldl -lc -lcamera2ndk -lmediandk -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/ -L$(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/aarch64-linux-android/35/ -L/data/data/com.termux/files/usr/lib' \
		ANDROID_NDK_HOME="$(ANDROID_NDK_HOME)" \
		PATH="${PATH}:${HOME}/go/bin" \
		GOFLAGS="$(GOBUILD_FLAGS) -ldflags=$(shell echo ${LINKER_FLAGS_ANDROID} | tr " " ",")" \
		fyne package $(FYNEBUILD_FLAGS) -release -os android/arm64 && mv streamforward.apk ../../build/streamforward-arm64.apk

ANDROID_AVD?=test_avd
ANDROID_SDK_ROOT?=$(HOME)/Android/Sdk
TERMUX_APK?=
TERMUX_API_APK?=
TERMUX_API_E2E?=1

.PHONY: android-emulator-start android-emulator-wait android-emulator-stop android-termux-install android-termux-api-install android-termux-setup android-test-termux-microphone-e2e android-test-microphone-e2e

android-emulator-start:
	$(ANDROID_SDK_ROOT)/emulator/emulator -avd $(ANDROID_AVD) -no-window -no-audio -no-boot-anim -netfast -gpu swiftshader_indirect

android-emulator-wait:
	adb wait-for-device
	adb shell getprop sys.boot_completed | grep -m1 1
	adb shell settings put global window_animation_scale 0
	adb shell settings put global transition_animation_scale 0
	adb shell settings put global animator_duration_scale 0

android-emulator-stop:
	adb emu kill

android-termux-install:
	@if [ -z "$(TERMUX_APK)" ]; then echo "TERMUX_APK is required"; exit 2; fi
	adb install -r "$(TERMUX_APK)"

android-termux-api-install:
	@if [ -z "$(TERMUX_API_APK)" ]; then echo "TERMUX_API_APK is required"; exit 2; fi
	adb install -r "$(TERMUX_API_APK)"

android-termux-setup:
	adb shell pm grant com.termux.api android.permission.RECORD_AUDIO
	adb shell pm grant com.termux android.permission.RECORD_AUDIO
	adb shell cmd appops set com.termux.api RECORD_AUDIO allow
	adb shell cmd appops set com.termux RECORD_AUDIO allow
	adb shell am start -n com.termux/com.termux.app.TermuxActivity
	adb shell am force-stop com.termux.api
	adb shell am start -n com.termux.api/.TermuxApiReceiver

android-test-termux-microphone-e2e:
	TERMUX_API_E2E=$(TERMUX_API_E2E) go test ./tests/e2e -tags test_e2e -run TermuxMicrophoneRecordE2E -v

android-test-microphone-e2e:
	go test ./tests/e2e -tags test_e2e -run AndroidMicrophoneRecordE2E -v

$(GOPATH)/bin/pkg-config-wrapper:
	go install github.com/xaionaro-go/pkg-config-wrapper@5dd443e6c18336416c49047e2ba0002e26a85278
