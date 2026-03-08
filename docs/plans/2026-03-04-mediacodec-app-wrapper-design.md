# MediaCodec E2E Test: Android App Wrapper

## Problem

MediaCodec NDK API requires an Android app context (JVM + binder to mediaserver). Native binaries run via `adb shell` or Termux lack this context, causing "Failed to create media format" errors on emulators.

## Solution

Minimal Android Gradle project that wraps Go test code as a shared library (`.so`), loaded inside an Activity. Run via `./gradlew connectedAndroidTest` or `adb shell am instrument`.

## Architecture

```
tests/e2e/android-app/
├── app/
│   ├── build.gradle.kts         # minSdk 24, multi-ABI (arm64-v8a, x86_64)
│   └── src/
│       ├── main/
│       │   ├── AndroidManifest.xml
│       │   └── java/.../MediaCodecTestActivity.java
│       └── androidTest/
│           └── java/.../MediaCodecInstrumentationTest.java
├── build.gradle.kts
├── settings.gradle.kts
└── jni/
    ├── arm64-v8a/libmctest.so
    └── x86_64/libmctest.so
```

## Components

### Go shared library (`libmctest.so`)

New file `tests/e2e/mediacodec_cshared.go` with build tag `android && cgo && ignore` (excluded from normal builds).

Exports C function:
```c
extern char* RunMediaCodecTests();  // Returns JSON: {"pass":true} or {"pass":false,"error":"..."}
```

Internally runs the same logic as `mediacodec_e2e_test.go`: auto-detection, HW device context creation, encode/decode round-trip.

### Java Activity

Loads `libmctest.so`, declares native method. Provides Android app context so MediaCodec binder connection is established.

### Instrumentation test

JUnit test that calls `RunMediaCodecTests()` via Activity, asserts pass. Reports errors on failure.

## Build flow

1. Cross-compile Go `.so` for arm64-v8a (Docker, existing infra) and x86_64 (host NDK)
2. Place `.so` files in `jni/{abi}/`
3. `./gradlew connectedAndroidTest` builds APK, installs, runs on connected device/emulator

### Makefile targets

```makefile
android-mediacodec-e2e-build    # Cross-compile .so for both ABIs
android-mediacodec-e2e-test     # gradlew connectedAndroidTest
```

## What is tested

1. MediaCodec auto-detection triggers hardware init path
2. HW device context is created (non-nil)
3. Encode→decode round-trip produces correct frame dimensions
4. Frames use hardware pixel format (PixelFormatMediacodec)

## Constraints

- minSdk 24 (matches existing NDK API level)
- Fat APK: arm64-v8a + x86_64
- No UI needed — Activity is just a host for native code
- Go code compiled separately, not via gomobile (known CGO issues)
