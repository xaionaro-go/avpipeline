//go:build android && cgo
// +build android,cgo

package android

import (
	"context"
	"encoding/binary"

	"github.com/xaionaro-go/avpipeline/logger"
)

// microphoneSilenceDetector tracks per-second audio levels and warns
// when the capture produces complete silence (all-zero samples),
// which typically indicates that Android sensor privacy is enabled.
type microphoneSilenceDetector struct {
	sampleRate     int
	reportInterval int64

	maxAbs                     int16
	framesTotal                int64
	silentFramesTotal          int64
	privacyDisableAttempted    bool
	disableSensorPrivacyOnSilence bool
}

func newSilenceDetector(cfg MicrophoneConfig) microphoneSilenceDetector {
	return microphoneSilenceDetector{
		sampleRate:                    cfg.SampleRate,
		reportInterval:                int64(cfg.SampleRate), // ~1 second
		disableSensorPrivacyOnSilence: cfg.DisableSensorPrivacyOnSilence,
	}
}

// update processes a chunk of PCM data and reports diagnostics
// every ~1 second of captured audio.
func (sd *microphoneSilenceDetector) update(
	ctx context.Context,
	pcm []byte,
	framesRead int64,
) {
	sd.updateMaxAbs(pcm)
	sd.framesTotal += framesRead
	if sd.framesTotal < sd.reportInterval {
		return
	}

	sd.reportDiagnostics(ctx)
	sd.checkSilence(ctx)

	sd.maxAbs = 0
	sd.framesTotal = 0
}

func (sd *microphoneSilenceDetector) updateMaxAbs(pcm []byte) {
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		if s < 0 {
			s = -s
		}
		if s > sd.maxAbs {
			sd.maxAbs = s
		}
	}
}

func (sd *microphoneSilenceDetector) reportDiagnostics(ctx context.Context) {
	logger.Debugf(ctx,
		"audio capture diagnostic: max_abs_sample=%d/%d frames_total=%d",
		sd.maxAbs, 32767, sd.framesTotal,
	)
}

func (sd *microphoneSilenceDetector) checkSilence(ctx context.Context) {
	if sd.maxAbs != 0 {
		sd.silentFramesTotal = 0
		sd.privacyDisableAttempted = false
		return
	}

	sd.silentFramesTotal += sd.framesTotal
	if sd.silentFramesTotal < int64(sd.sampleRate) {
		return
	}

	logger.Warnf(ctx,
		"audio capture: complete silence for %.1fs (microphone privacy enabled?)",
		float64(sd.silentFramesTotal)/float64(sd.sampleRate),
	)

	if !sd.disableSensorPrivacyOnSilence || sd.privacyDisableAttempted {
		return
	}
	sd.privacyDisableAttempted = true
	disableSensorPrivacy(ctx)
}
