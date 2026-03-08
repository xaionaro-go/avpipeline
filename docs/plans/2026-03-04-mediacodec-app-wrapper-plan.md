# MediaCodec E2E Test: Android App Wrapper — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run MediaCodec E2E tests inside a proper Android app context so the NDK MediaCodec API works on emulators and real devices.

**Architecture:** Go test code compiled as a C-shared library (`libmctest.so`) with FFmpeg statically linked. Loaded by a minimal Android app via JNI. Instrumentation test invokes the native function and asserts pass. Fat APK supports arm64-v8a + x86_64.

**Tech Stack:** Go (c-shared buildmode), Android Gradle (Kotlin DSL), JNI, NDK 28, minSdk 24, JUnit4

**Important context:**
- `.gitignore` excludes `/tests` — use `git add -f` for all files under `tests/`
- Existing Docker infra handles ARM64 cross-compilation
- x86_64 can be compiled natively inside Termux on emulator (Go 1.26 + FFmpeg already installed)
- The emulator is Pixel_7_API_35 (x86_64), started with `$ANDROID_HOME/emulator/emulator -avd Pixel_7_API_35`
- Termux is installed on the emulator; access via `adb shell su 10215 sh -c '...'`

---

### Task 0: Spike — verify if native binary works from /data/local/tmp/

Before building the full app wrapper, check if a simple C binary using MediaCodec NDK works when run from `/data/local/tmp/` (AOSP's native test location). If it works, the app wrapper may be unnecessary.

**Files:**
- Create: `/tmp/mc_probe.c` (temporary, not committed)

**Step 1: Write a minimal C MediaCodec probe**

```c
#include <stdio.h>
#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>

int main() {
    AMediaFormat *fmt = AMediaFormat_new();
    if (!fmt) { printf("FAIL: AMediaFormat_new\n"); return 1; }
    printf("OK: AMediaFormat created\n");

    AMediaFormat_setString(fmt, AMEDIAFORMAT_KEY_MIME, "video/avc");
    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_WIDTH, 128);
    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_HEIGHT, 128);
    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_COLOR_FORMAT, 19);

    AMediaCodec *dec = AMediaCodec_createDecoderByType("video/avc");
    if (!dec) { printf("FAIL: decoder null\n"); }
    else {
        int rc = AMediaCodec_configure(dec, fmt, NULL, NULL, 0);
        printf("Decoder configure: %d (%s)\n", rc, rc==0 ? "OK" : "FAIL");
        if (rc == 0) { AMediaCodec_start(dec); AMediaCodec_stop(dec); }
        AMediaCodec_delete(dec);
    }

    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_BIT_RATE, 500000);
    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_FRAME_RATE, 30);
    AMediaFormat_setInt32(fmt, AMEDIAFORMAT_KEY_I_FRAME_INTERVAL, 1);

    AMediaCodec *enc = AMediaCodec_createEncoderByType("video/avc");
    if (!enc) { printf("FAIL: encoder null\n"); }
    else {
        int rc = AMediaCodec_configure(enc, fmt, NULL, NULL, 1);
        printf("Encoder configure: %d (%s)\n", rc, rc==0 ? "OK" : "FAIL");
        if (rc == 0) { AMediaCodec_start(enc); AMediaCodec_stop(enc); }
        AMediaCodec_delete(enc);
    }

    AMediaFormat_delete(fmt);
    printf("Done.\n");
    return 0;
}
```

**Step 2: Cross-compile and run on emulator**

```bash
# Compile for x86_64 Android
NDK=$ANDROID_HOME/ndk/28.0.13004108
$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android35-clang \
    /tmp/mc_probe.c -o /tmp/mc_probe -lmediandk -llog

# Push and run
adb push /tmp/mc_probe /data/local/tmp/mc_probe
adb shell chmod 755 /data/local/tmp/mc_probe
adb shell /data/local/tmp/mc_probe
```

**Step 3: Evaluate result**

- If decoder AND encoder configure both return `OK`: MediaCodec works from native binary. Simplify remaining tasks — skip the app wrapper, just push Go test binary to `/data/local/tmp/` instead.
- If `FAIL`: Proceed with the full app wrapper approach (Tasks 1-7).

**Step 4: Clean up**

```bash
adb shell rm /data/local/tmp/mc_probe
rm /tmp/mc_probe /tmp/mc_probe.c
```

---

### Task 1: Create Go C-shared library entry point

**Files:**
- Create: `tests/e2e/mediacodec_cshared.go`

**Step 1: Write the C-shared entry point**

This file is excluded from normal builds via `//go:build ignore`. It's only compiled explicitly via `go build -buildmode=c-shared`.

```go
//go:build ignore

package main

/*
#include <stdlib.h>
#include <jni.h>

// Forward declaration of the Go function
extern char* runMediaCodecTestsC();

// JNI wrapper that converts C string to jstring
static jstring Java_RunMediaCodecTests(JNIEnv *env, jclass cls) {
    char *result = runMediaCodecTestsC();
    jstring jresult = (*env)->NewStringUTF(env, result);
    free(result);
    return jresult;
}

// Register native methods on library load
static JNINativeMethod methods[] = {
    {"RunMediaCodecTests", "()Ljava/lang/String;", (void *)Java_RunMediaCodecTests},
};

JNIEXPORT jint JNI_OnLoad(JavaVM *vm, void *reserved) {
    JNIEnv *env;
    if ((*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6) != JNI_OK) {
        return JNI_ERR;
    }
    jclass cls = (*env)->FindClass(env, "com/avpipeline/mctest/MediaCodecTestActivity");
    if (cls == NULL) return JNI_ERR;
    (*env)->RegisterNatives(env, cls, methods, sizeof(methods)/sizeof(methods[0]));
    return JNI_VERSION_1_6;
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/logger"
)

type testResult struct {
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
	Log   string `json:"log,omitempty"`
}

//export runMediaCodecTestsC
func runMediaCodecTestsC() *C.char {
	result := runTests()
	b, _ := json.Marshal(result)
	return C.CString(string(b))
}

func runTests() testResult {
	l := logrus.Default().WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	// Test 1: Auto-detect HW device context for decoder
	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(256)
	cp.SetHeight(256)

	dec, err := codec.NewDecoder(ctx, codec.DecoderInput{
		CodecParameters: cp,
		CodecName:       "h264_mediacodec",
	})
	if err != nil {
		return testResult{Pass: false, Error: fmt.Sprintf("decoder creation failed: %v", err)}
	}
	defer func() { _ = dec.Close(ctx) }()

	if dec.HardwareDeviceContext() == nil {
		return testResult{Pass: false, Error: "decoder HardwareDeviceContext is nil"}
	}

	// Test 2: Auto-detect HW device context for encoder
	encCP := astiav.AllocCodecParameters()
	defer encCP.Free()
	encCP.SetMediaType(astiav.MediaTypeVideo)
	encCP.SetCodecID(astiav.CodecIDH264)
	encCP.SetWidth(256)
	encCP.SetHeight(256)

	enc, err := codec.NewEncoder(ctx, codec.CodecParams{
		CodecName:       "h264_mediacodec",
		CodecParameters: encCP,
		TimeBase:        astiav.NewRational(1, 30),
	})
	if err != nil {
		return testResult{Pass: false, Error: fmt.Sprintf("encoder creation failed: %v", err)}
	}
	defer func() { _ = enc.Close(ctx) }()

	if enc.HardwareDeviceContext() == nil {
		return testResult{Pass: false, Error: "encoder HardwareDeviceContext is nil"}
	}

	// Test 3: Encode frames
	pixFmt := enc.CodecContext().PixelFormat()
	timeBase := enc.CodecContext().TimeBase()
	var packets []*astiav.Packet
	for i := int64(0); i < 10; i++ {
		frame := astiav.AllocFrame()
		defer frame.Free()
		frame.SetWidth(256)
		frame.SetHeight(256)
		frame.SetPixelFormat(pixFmt)
		if err := frame.AllocBuffer(0); err != nil {
			return testResult{Pass: false, Error: fmt.Sprintf("AllocBuffer: %v", err)}
		}
		frame.SetPts(i * int64(timeBase.Den()) / (30 * int64(timeBase.Num())))
		frame.SetDuration(int64(timeBase.Den()) / (30 * int64(timeBase.Num())))

		if err := enc.SendFrame(ctx, frame); err != nil {
			return testResult{Pass: false, Error: fmt.Sprintf("SendFrame: %v", err)}
		}

		pkt := astiav.AllocPacket()
		for {
			if err := enc.ReceivePacket(ctx, pkt); err != nil {
				break
			}
			copyPkt := astiav.AllocPacket()
			if err := copyPkt.Ref(pkt); err != nil {
				return testResult{Pass: false, Error: fmt.Sprintf("Ref: %v", err)}
			}
			packets = append(packets, copyPkt)
			pkt.Unref()
		}
	}
	if len(packets) == 0 {
		return testResult{Pass: false, Error: "encoder produced 0 packets"}
	}

	// Test 4: Decode packets
	decCP := astiav.AllocCodecParameters()
	defer decCP.Free()
	if err := enc.CodecContext().ToCodecParameters(decCP); err != nil {
		return testResult{Pass: false, Error: fmt.Sprintf("ToCodecParameters: %v", err)}
	}

	dec2, err := codec.NewDecoder(ctx, codec.DecoderInput{
		CodecParameters: decCP,
		CodecName:       "h264_mediacodec",
	})
	if err != nil {
		return testResult{Pass: false, Error: fmt.Sprintf("decoder2 creation: %v", err)}
	}
	defer func() { _ = dec2.Close(ctx) }()

	var decoded int
	for _, pkt := range packets {
		if err := dec2.SendPacket(ctx, pkt); err != nil {
			continue
		}
		for {
			frame := astiav.AllocFrame()
			if err := dec2.ReceiveFrame(ctx, frame); err != nil {
				frame.Free()
				break
			}
			decoded++
			if frame.Width() != 256 || frame.Height() != 256 {
				frame.Free()
				return testResult{Pass: false, Error: fmt.Sprintf("wrong dimensions: %dx%d", frame.Width(), frame.Height())}
			}
			frame.Free()
		}
	}
	if decoded == 0 {
		return testResult{Pass: false, Error: "decoder produced 0 frames"}
	}

	return testResult{
		Pass: true,
		Log:  fmt.Sprintf("encoded %d packets, decoded %d frames", len(packets), decoded),
	}
}

func main() {}
```

**Step 2: Verify it compiles (syntax check only, not for Android yet)**

```bash
# This won't link on host (no Android libs), but checks syntax:
go vet ./tests/e2e/mediacodec_cshared.go 2>&1 || echo "Expected: vet may fail for c-shared with ignore tag"
```

**Step 3: Commit**

```bash
git add -f tests/e2e/mediacodec_cshared.go
git commit -m "Add Go C-shared entry point for MediaCodec E2E tests"
```

---

### Task 2: Build script for x86_64 shared library (Termux-based)

**Files:**
- Create: `tests/e2e/android-app/build-so-x86_64.sh`

**Step 1: Write the build script**

This script runs inside Termux on the emulator. It compiles `mediacodec_cshared.go` as a c-shared library.

```bash
#!/bin/bash
set -euo pipefail

# Run inside Termux on x86_64 emulator.
# Prerequisites: go, clang, pkg-config, ffmpeg (all via `pkg install`).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OUT_DIR="$SCRIPT_DIR/app/src/main/jniLibs/x86_64"

mkdir -p "$OUT_DIR"

cd "$PROJECT_ROOT"

# jni.h is in the NDK sysroot (Termux includes it in its clang package)
CGO_ENABLED=1 \
  go build \
    -buildmode=c-shared \
    -o "$OUT_DIR/libmctest.so" \
    ./tests/e2e/mediacodec_cshared.go

# Strip debug info to reduce size
strip "$OUT_DIR/libmctest.so" 2>/dev/null || true

echo "Built: $OUT_DIR/libmctest.so"
ls -lh "$OUT_DIR/libmctest.so"
```

**Step 2: Commit**

```bash
git add -f tests/e2e/android-app/build-so-x86_64.sh
chmod +x tests/e2e/android-app/build-so-x86_64.sh
git commit -m "Add x86_64 shared library build script for MediaCodec E2E"
```

---

### Task 3: Create minimal Android Gradle project

**Files:**
- Create: `tests/e2e/android-app/settings.gradle.kts`
- Create: `tests/e2e/android-app/build.gradle.kts`
- Create: `tests/e2e/android-app/app/build.gradle.kts`
- Create: `tests/e2e/android-app/gradle.properties`

**Step 1: Write root settings.gradle.kts**

```kotlin
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolution {
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "mediacodec-e2e"
include(":app")
```

**Step 2: Write root build.gradle.kts**

```kotlin
plugins {
    id("com.android.application") version "8.7.3" apply false
}
```

**Step 3: Write app/build.gradle.kts**

```kotlin
plugins {
    id("com.android.application")
}

android {
    namespace = "com.avpipeline.mctest"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.avpipeline.mctest"
        minSdk = 24
        targetSdk = 35
        versionCode = 1
        versionName = "1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        ndk {
            abiFilters += listOf("arm64-v8a", "x86_64")
        }
    }

    sourceSets {
        getByName("main") {
            // Load pre-built .so from jniLibs
            jniLibs.srcDirs("src/main/jniLibs")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_1_8
        targetCompatibility = JavaVersion.VERSION_1_8
    }
}

dependencies {
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test:rules:1.6.1")
    androidTestImplementation("junit:junit:4.13.2")
}
```

**Step 4: Write gradle.properties**

```properties
android.useAndroidX=true
org.gradle.jvmargs=-Xmx1536m
```

**Step 5: Generate Gradle wrapper**

```bash
cd tests/e2e/android-app
# Download and set up gradle wrapper (requires gradle or manual download)
# Using the Gradle wrapper JAR approach:
mkdir -p gradle/wrapper
cat > gradle/wrapper/gradle-wrapper.properties << 'EOF'
distributionBase=GRADLE_USER_HOME
distributionPath=wrapper/dists
distributionUrl=https\://services.gradle.org/distributions/gradle-8.9-bin.zip
networkTimeout=10000
validateDistributionUrl=true
zipStoreBase=GRADLE_USER_HOME
zipStorePath=wrapper/dists
EOF
```

Then download gradlew:
```bash
curl -sL https://raw.githubusercontent.com/gradle/gradle/v8.9.0/gradlew -o gradlew
chmod +x gradlew
curl -sL https://raw.githubusercontent.com/gradle/gradle/v8.9.0/gradle/wrapper/gradle-wrapper.jar -o gradle/wrapper/gradle-wrapper.jar
```

**Step 6: Commit**

```bash
git add -f tests/e2e/android-app/settings.gradle.kts \
         tests/e2e/android-app/build.gradle.kts \
         tests/e2e/android-app/app/build.gradle.kts \
         tests/e2e/android-app/gradle.properties \
         tests/e2e/android-app/gradlew \
         tests/e2e/android-app/gradle/
git commit -m "Add minimal Android Gradle project for MediaCodec E2E"
```

---

### Task 4: Create Java Activity and JNI bridge

**Files:**
- Create: `tests/e2e/android-app/app/src/main/AndroidManifest.xml`
- Create: `tests/e2e/android-app/app/src/main/java/com/avpipeline/mctest/MediaCodecTestActivity.java`

**Step 1: Write AndroidManifest.xml**

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <application
        android:label="MCTest"
        android:allowBackup="false">
        <activity
            android:name=".MediaCodecTestActivity"
            android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
```

**Step 2: Write MediaCodecTestActivity.java**

```java
package com.avpipeline.mctest;

import android.app.Activity;
import android.os.Bundle;
import android.util.Log;

public class MediaCodecTestActivity extends Activity {
    private static final String TAG = "MCTest";

    static {
        System.loadLibrary("mctest");
    }

    // JNI — registered via JNI_OnLoad in Go C-shared library
    public static native String RunMediaCodecTests();

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Log.i(TAG, "MediaCodecTestActivity created");
    }
}
```

**Step 3: Commit**

```bash
git add -f tests/e2e/android-app/app/src/main/AndroidManifest.xml \
         tests/e2e/android-app/app/src/main/java/com/avpipeline/mctest/MediaCodecTestActivity.java
git commit -m "Add Android Activity and JNI bridge for MediaCodec E2E"
```

---

### Task 5: Create instrumentation test

**Files:**
- Create: `tests/e2e/android-app/app/src/androidTest/java/com/avpipeline/mctest/MediaCodecInstrumentationTest.java`

**Step 1: Write the instrumentation test**

```java
package com.avpipeline.mctest;

import static org.junit.Assert.*;

import android.util.Log;
import androidx.test.rule.ActivityTestRule;
import org.json.JSONObject;
import org.junit.Rule;
import org.junit.Test;

public class MediaCodecInstrumentationTest {
    private static final String TAG = "MCTest";

    @Rule
    public ActivityTestRule<MediaCodecTestActivity> activityRule =
        new ActivityTestRule<>(MediaCodecTestActivity.class);

    @Test
    public void testMediaCodecRoundTrip() throws Exception {
        String json = MediaCodecTestActivity.RunMediaCodecTests();
        Log.i(TAG, "Test result: " + json);

        JSONObject result = new JSONObject(json);
        if (result.has("log")) {
            Log.i(TAG, "Test log: " + result.getString("log"));
        }
        assertTrue("MediaCodec test failed: " + result.optString("error", "unknown"),
            result.getBoolean("pass"));
    }
}
```

**Step 2: Commit**

```bash
git add -f tests/e2e/android-app/app/src/androidTest/java/com/avpipeline/mctest/MediaCodecInstrumentationTest.java
git commit -m "Add MediaCodec instrumentation test"
```

---

### Task 6: Add Makefile targets

**Files:**
- Modify: `Makefile` (append new targets)

**Step 1: Add targets to Makefile**

Append after the existing `android-test-microphone-e2e` target:

```makefile
# MediaCodec E2E via Android app wrapper
.PHONY: android-mediacodec-e2e-build-x86_64 android-mediacodec-e2e-test

android-mediacodec-e2e-build-x86_64:
	@echo "Building libmctest.so for x86_64 inside Termux on emulator..."
	adb shell "run-as com.termux sh -c 'cd ~/avpipeline && bash tests/e2e/android-app/build-so-x86_64.sh'"
	@echo "Pulling built .so from device..."
	mkdir -p tests/e2e/android-app/app/src/main/jniLibs/x86_64
	adb shell "run-as com.termux cat ~/avpipeline/tests/e2e/android-app/app/src/main/jniLibs/x86_64/libmctest.so" \
		> tests/e2e/android-app/app/src/main/jniLibs/x86_64/libmctest.so

android-mediacodec-e2e-test: android-mediacodec-e2e-build-x86_64
	cd tests/e2e/android-app && \
		ANDROID_HOME=$(ANDROID_SDK_ROOT) \
		./gradlew connectedAndroidTest
```

**Step 2: Commit**

```bash
git add Makefile
git commit -m "Add Makefile targets for MediaCodec E2E app wrapper"
```

---

### Task 7: End-to-end verification

**Step 1: Ensure emulator is running**

```bash
adb devices  # Should show emulator
# If not:
$ANDROID_HOME/emulator/emulator -avd Pixel_7_API_35 -no-window -no-audio -gpu swiftshader_indirect &
adb wait-for-device
adb shell getprop sys.boot_completed | grep -m1 1
```

**Step 2: Sync code to Termux**

```bash
# Push updated source to Termux
adb push . /data/local/tmp/avpipeline-sync
adb shell "su 10215 sh -c 'cp -r /data/local/tmp/avpipeline-sync/* ~/avpipeline/'"
```

**Step 3: Build the shared library on device**

```bash
adb shell "su 10215 sh -c 'cd ~/avpipeline && bash tests/e2e/android-app/build-so-x86_64.sh'"
```

Expected: `Built: .../jniLibs/x86_64/libmctest.so`

**Step 4: Pull .so and run Gradle tests**

```bash
mkdir -p tests/e2e/android-app/app/src/main/jniLibs/x86_64
adb shell "su 10215 sh -c 'cat ~/avpipeline/tests/e2e/android-app/app/src/main/jniLibs/x86_64/libmctest.so'" \
    > tests/e2e/android-app/app/src/main/jniLibs/x86_64/libmctest.so

cd tests/e2e/android-app
ANDROID_HOME=$HOME/Android/Sdk ./gradlew connectedAndroidTest
```

Expected: `BUILD SUCCESSFUL` with test `testMediaCodecRoundTrip` passing.

**Step 5: If tests fail, check logcat**

```bash
adb logcat -d -s MCTest:* | tail -50
```

**Step 6: Commit any fixes, then final commit**

```bash
git add -f tests/e2e/android-app/
git commit -m "MediaCodec E2E tests passing on Android emulator"
```

---

## Notes

- **`.gitignore` excludes `/tests`**: All `git add` commands use `-f` flag.
- **The `.so` files are NOT committed** — they're built on-device and pulled. Add to `.gitignore`:
  ```
  tests/e2e/android-app/app/src/main/jniLibs/
  tests/e2e/android-app/.gradle/
  tests/e2e/android-app/app/build/
  tests/e2e/android-app/build/
  ```
- **JNI function naming**: Resolved via `JNI_OnLoad` + `RegisterNatives` in the Go C preamble. Go exports `runMediaCodecTestsC`, the C JNI wrapper converts the result to `jstring` and is registered as the Java `RunMediaCodecTests` method.
- **ARM64 build**: Not covered in this plan. Uses existing Docker infrastructure. Add as a follow-up task.
