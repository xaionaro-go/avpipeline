//go:build android

// termux_microphone.go implements a Termux microphone kernel.

package android

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/avpipeline/types/astiav"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/xsync"
)

type TermuxMicrophoneConfig struct {
	FilePath      string
	Limit         time.Duration
	Encoder       string
	BitrateKbps   int
	SampleRate    int
	Channels      int
	PollInterval  time.Duration
	DialTimeout   time.Duration
	DeleteOnClose bool
	InputConfig   kernel.InputConfig
}

type TermuxMicrophone struct {
	*closuresignaler.ClosureSignaler
	Config TermuxMicrophoneConfig
	Locker xsync.Mutex
	input  *kernel.Input
	client *termuxAPIClient
}

var _ kernel.Abstract = (*TermuxMicrophone)(nil)

func NewTermuxMicrophone(ctx context.Context, cfg TermuxMicrophoneConfig) *TermuxMicrophone {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if !cfg.DeleteOnClose {
		cfg.DeleteOnClose = true
	}
	return &TermuxMicrophone{
		ClosureSignaler: closuresignaler.New(),
		Config:          cfg,
		client:          newTermuxAPIClient(),
	}
}

func (k *TermuxMicrophone) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *TermuxMicrophone) String() string {
	if k == nil {
		return "TermuxMicrophone(<nil>)"
	}
	return "TermuxMicrophone"
}

func (k *TermuxMicrophone) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close()")
	defer func() { logger.Debugf(ctx, "/Close(): %v", _err) }()
	k.ClosureSignaler.Close(ctx)
	return xsync.DoA1R1(ctx, &k.Locker, k.closeLocked, ctx)
}

func (k *TermuxMicrophone) closeLocked(ctx context.Context) error {
	if k.input != nil {
		err := k.input.Close(ctx)
		k.input = nil
		return err
	}
	return nil
}

func (k *TermuxMicrophone) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	_ = ctx
	_ = input
	_ = outputCh
	return kerneltypes.ErrUnexpectedInputType{}
}

func (k *TermuxMicrophone) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Generate")
	defer func() { logger.Debugf(ctx, "/Generate: %v", _err) }()

	if err := k.ensureTermuxAvailable(ctx); err != nil {
		return err
	}

	filePath, err := k.prepareFilePath()
	if err != nil {
		return err
	}
	if k.Config.DeleteOnClose {
		defer func() {
			if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				logger.Warnf(ctx, "unable to delete recording file %q: %v", filePath, err)
			}
		}()
	}

	if err := k.startRecording(ctx, filePath); err != nil {
		return err
	}
	defer k.cleanupRecording(filePath)

	if err := k.waitForRecordingFileReady(ctx, filePath); err != nil {
		return err
	}
	return k.generateStreamingFromFile(ctx, filePath, outputCh)
}

func (k *TermuxMicrophone) setInputLocked(ctx context.Context, input *kernel.Input) error {
	_ = ctx
	if k.input != nil {
		_ = k.input.Close(ctx)
	}
	k.input = input
	return nil
}

func (k *TermuxMicrophone) generateStreamingFromFile(
	ctx context.Context,
	filePath string,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	inputCfg := k.Config.InputConfig
	if inputCfg.ForceRealTime == nil {
		forceRealTime := false
		inputCfg.ForceRealTime = &forceRealTime
	}
	inputCfg.AutoClose = false

	streamingCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	inputCfg.CustomOptions = append(inputCfg.CustomOptions, globaltypes.DictionaryItem{Key: "follow", Value: "1"})
	inputKernel, err := kernel.NewInputFromURL(streamingCtx, filePath, secret.New(""), inputCfg)
	if err != nil {
		return fmt.Errorf("unable to open recorded file %q: %w", filePath, err)
	}
	if err := xsync.DoA2R1(ctx, &k.Locker, k.setInputLocked, ctx, inputKernel); err != nil {
		_ = inputKernel.Close(ctx)
		return err
	}
	defer func() {
		_ = inputKernel.Close(ctx)
	}()

	if err := inputKernel.Generate(streamingCtx, outputCh); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

func (k *TermuxMicrophone) waitForRecordingFileReady(ctx context.Context, filePath string) error {
	ticker := time.NewTicker(k.Config.PollInterval)
	defer ticker.Stop()

	for {
		info, err := os.Stat(filePath)
		if err == nil && info.Size() > 0 {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("unable to stat recording file %q: %w", filePath, err)
		}

		recorderInfo, err := k.getRecordingInfo(ctx)
		if err != nil {
			logger.Warnf(ctx, "unable to read termux microphone info: %v", err)
		} else if !recorderInfo.IsRecording {
			return fmt.Errorf("recording stopped before file %q became available", filePath)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return io.EOF
		case <-ticker.C:
		}
	}
}

func (k *TermuxMicrophone) cleanupRecording(filePath string) {
	timeout := 5 * time.Second
	if k.Config.DialTimeout > 0 {
		timeout = k.Config.DialTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	info, err := k.getRecordingInfo(ctx)
	if err != nil || !info.IsRecording {
		return
	}
	_ = k.stopAndWait(ctx, filePath, nil)
}

func (k *TermuxMicrophone) waitForRecordingCompletion(ctx context.Context) (bool, error) {
	info, err := k.getRecordingInfo(ctx)
	if err != nil {
		logger.Warnf(ctx, "unable to read termux microphone info: %v", err)
		return false, nil
	}
	if !info.IsRecording {
		return true, nil
	}
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case <-k.CloseChan():
		return true, io.EOF
	case <-time.After(k.Config.PollInterval):
	}
	return false, nil
}

func (k *TermuxMicrophone) applyRecordingTimeBase(out packetorframe.OutputUnion) {
	if out.Packet != nil {
		out.Packet.SetTimeBase(astiav.RationalToAstiav(globaltypes.Rational{Num: 1, Den: 1000}))
		return
	}
	if out.Frame != nil {
		out.Frame.SetTimeBase(astiav.RationalToAstiav(globaltypes.Rational{Num: 1, Den: 1000}))
	}
}

func (k *TermuxMicrophone) prepareFilePath() (string, error) {
	filePath := strings.TrimSpace(k.Config.FilePath)
	if filePath == "" {
		base := fmt.Sprintf("termux-microphone-%d", time.Now().UnixNano())
		filePath = filepath.Join(os.TempDir(), base)
	}

	if filepath.Ext(filePath) == "" {
		if ext := termuxMicrophoneExtension(k.Config.Encoder); ext != "" {
			filePath += ext
		}
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("unable to resolve recording path %q: %w", filePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("unable to create recording directory: %w", err)
	}
	return absPath, nil
}

func (k *TermuxMicrophone) startRecording(ctx context.Context, filePath string) error {
	cmd := termuxAPICommand{
		MethodName: "MicRecorder",
		Action:     "record",
		Extras:     k.recordExtras(filePath),
	}
	_, err := k.callTermux(ctx, cmd)
	if err != nil {
		return err
	}
	return nil
}

func (k *TermuxMicrophone) recordExtras(filePath string) []termuxAPIExtra {
	var extras []termuxAPIExtra
	if filePath != "" {
		extras = append(extras, termuxAPIExtra{
			Key:   "file",
			Value: termuxAPIString(filePath),
		})
	}
	if k.Config.Limit > 0 {
		limitMs := int(k.Config.Limit.Milliseconds())
		extras = append(extras, termuxAPIExtra{
			Key:   "limit",
			Value: termuxAPIInt(limitMs),
		})
	}
	if k.Config.Encoder != "" {
		extras = append(extras, termuxAPIExtra{
			Key:   "encoder",
			Value: termuxAPIString(k.Config.Encoder),
		})
	}
	if k.Config.BitrateKbps > 0 {
		bitrate := k.Config.BitrateKbps * 1000
		extras = append(extras, termuxAPIExtra{
			Key:   "bitrate",
			Value: termuxAPIInt(bitrate),
		})
	}
	if k.Config.SampleRate > 0 {
		extras = append(extras, termuxAPIExtra{
			Key:   "srate",
			Value: termuxAPIInt(k.Config.SampleRate),
		})
	}
	if k.Config.Channels > 0 {
		extras = append(extras, termuxAPIExtra{
			Key:   "channels",
			Value: termuxAPIInt(k.Config.Channels),
		})
	}
	return extras
}

func (k *TermuxMicrophone) waitForRecording(ctx context.Context, filePath string) error {
	if k.Config.Limit <= 0 {
		select {
		case <-ctx.Done():
			return k.stopAndWait(ctx, filePath, ctx.Err())
		case <-k.CloseChan():
			return k.stopAndWait(ctx, filePath, context.Canceled)
		}
	}

	ticker := time.NewTicker(k.Config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return k.stopAndWait(ctx, filePath, ctx.Err())
		case <-k.CloseChan():
			return k.stopAndWait(ctx, filePath, context.Canceled)
		case <-ticker.C:
			info, err := k.getRecordingInfo(ctx)
			if err != nil {
				logger.Warnf(ctx, "unable to read termux microphone info: %v", err)
				continue
			}
			if !info.IsRecording {
				return nil
			}
		}
	}
}

func (k *TermuxMicrophone) stopAndWait(ctx context.Context, filePath string, retErr error) error {
	cmd := termuxAPICommand{MethodName: "MicRecorder", Action: "quit"}
	if _, err := k.callTermux(ctx, cmd); err != nil {
		logger.Warnf(ctx, "unable to stop termux microphone recording: %v", err)
	}

	ticker := time.NewTicker(k.Config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return retErr
		case <-ticker.C:
			info, err := k.getRecordingInfo(ctx)
			if err != nil {
				logger.Warnf(ctx, "unable to read termux microphone info: %v", err)
				continue
			}
			if !info.IsRecording {
				return retErr
			}
			if info.OutputFile != "" && filePath != info.OutputFile {
				logger.Warnf(ctx, "termux microphone reported output file change: %q -> %q", filePath, info.OutputFile)
			}
		}
	}
}

func (k *TermuxMicrophone) getRecordingInfo(ctx context.Context) (termuxMicrophoneInfo, error) {
	cmd := termuxAPICommand{MethodName: "MicRecorder", Action: "info"}
	result, err := k.callTermux(ctx, cmd)
	if err != nil {
		return termuxMicrophoneInfo{}, err
	}
	return parseTermuxMicrophoneInfo(result.Raw)
}

func (k *TermuxMicrophone) callTermux(ctx context.Context, cmd termuxAPICommand) (termuxAPIResult, error) {
	client := k.client
	if client == nil {
		client = newTermuxAPIClient()
	}
	client.DialTimeout = k.Config.DialTimeout
	result, err := client.Call(ctx, cmd)
	if err != nil {
		return termuxAPIResult{}, err
	}
	logTermuxResult(ctx, cmd.MethodName, result)
	return result, nil
}

type termuxMicrophoneInfo struct {
	IsRecording bool   `json:"isRecording"`
	OutputFile  string `json:"outputFile"`
}

func parseTermuxMicrophoneInfo(output string) (termuxMicrophoneInfo, error) {
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start == -1 || end == -1 || start >= end {
		return termuxMicrophoneInfo{}, ErrTermuxAPIResponse{Message: strings.TrimSpace(output)}
	}
	var info termuxMicrophoneInfo
	if err := json.Unmarshal([]byte(output[start:end+1]), &info); err != nil {
		return termuxMicrophoneInfo{}, ErrTermuxAPIResponse{Err: err, Message: strings.TrimSpace(output)}
	}
	return info, nil
}

func termuxMicrophoneExtension(encoder string) string {
	switch strings.ToLower(strings.TrimSpace(encoder)) {
	case "aac":
		return ".m4a"
	case "amr_nb", "amr_wb":
		return ".3gp"
	case "opus":
		return ".ogg"
	default:
		return ""
	}
}

func (k *TermuxMicrophone) ensureTermuxAvailable(ctx context.Context) error {
	cmd := termuxAPICommand{MethodName: "MicRecorder", Action: "info"}
	_, err := k.callTermux(ctx, cmd)
	if err != nil {
		var unavailable ErrTermuxAPIUnavailable
		if errors.As(err, &unavailable) {
			return err
		}
	}
	return nil
}
