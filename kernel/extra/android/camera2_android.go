//go:build android && cgo
// +build android,cgo

package android

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/AndroidGoLab/ndk/camera"
	cameracapi "github.com/AndroidGoLab/ndk/capi/camera"
	"github.com/AndroidGoLab/ndk/looper"
	"github.com/AndroidGoLab/ndk/media"
	windowpkg "github.com/AndroidGoLab/ndk/window"
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	camera2NDKSessionReadyTimeout = 2 * time.Second
	camera2NDKLooperPollInterval  = 16 * time.Millisecond
	camera2NDKReaderUsageCPURead  = uint64(0x3)
)

type Camera2NDK struct {
	*closuresignaler.ClosureSignaler
	Config        Camera2NDKConfig
	Locker        xsync.Mutex
	imageLocker   sync.Mutex
	streamInfo    *frame.StreamInfo
	codecParams   *astiav.CodecParameters
	formatContext *astiav.FormatContext

	manager       *camera.Manager
	device        *camera.Device
	reader        *media.ImageReader
	nativeWindow  *windowpkg.Window
	outputTarget  *camera.OutputTarget
	sessionOutput *camera.SessionOutput
	outputs       *camera.SessionOutputContainer
	request       *camera.CaptureRequest
	session       *camera.CaptureSession
	sessionClosed <-chan struct{}

	deviceStateCallbackID  uintptr
	sessionStateCallbackID uintptr
}

var _ kerneltypes.Abstract = (*Camera2NDK)(nil)

func NewCamera2NDK(ctx context.Context, cfg Camera2NDKConfig) (*Camera2NDK, error) {
	cfg, err := normalizeCamera2NDKConfig(cfg)
	if err != nil {
		return nil, err
	}
	codecParams, err := camera2NDKCodecParameters(ctx, cfg)
	if err != nil {
		return nil, err
	}
	formatContext := astiav.AllocFormatContext()
	if formatContext == nil {
		return nil, fmt.Errorf("unable to allocate format context")
	}
	internal.SetFinalizerFree(ctx, formatContext)

	k := &Camera2NDK{
		ClosureSignaler: closuresignaler.New(),
		Config:          cfg,
		codecParams:     codecParams,
		formatContext:   formatContext,
	}
	k.streamInfo = frame.BuildStreamInfo(
		k,
		codecParams,
		0,
		1,
		astiav.NewRational(1, camera2NDKTimeBaseDen),
		0,
		nil,
	)

	stream := k.formatContext.NewStream(nil)
	if stream == nil {
		return nil, fmt.Errorf("unable to create format stream")
	}
	codecParams.Copy(stream.CodecParameters())
	stream.SetTimeBase(astiav.NewRational(1, camera2NDKTimeBaseDen))
	stream.SetIndex(0)

	return k, nil
}

func camera2NDKCodecParameters(
	ctx context.Context,
	cfg Camera2NDKConfig,
) (*astiav.CodecParameters, error) {
	codecParams := astiav.AllocCodecParameters()
	if codecParams == nil {
		return nil, fmt.Errorf("unable to allocate codec parameters")
	}
	internal.SetFinalizerFree(ctx, codecParams)
	codecParams.SetMediaType(astiav.MediaTypeVideo)
	codecParams.SetCodecID(astiav.CodecIDRawvideo)
	codecParams.SetWidth(int(cfg.Width))
	codecParams.SetHeight(int(cfg.Height))
	codecParams.SetPixelFormat(camera2NDKPixelFormat(cfg.ImageFormat))
	return codecParams, nil
}

func (k *Camera2NDK) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *Camera2NDK) String() string {
	if k == nil {
		return "Camera2NDK(<nil>)"
	}
	return "Camera2NDK"
}

func (k *Camera2NDK) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	if k == nil {
		return
	}
	k.Locker.Do(ctx, func() {
		callback(k.formatContext)
	})
}

func (k *Camera2NDK) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	if k == nil {
		return nil
	}
	k.ClosureSignaler.Close(ctx)
	return xsync.DoA1R1(ctx, &k.Locker, k.closeLocked, ctx)
}

func (k *Camera2NDK) CloseChan() <-chan struct{} {
	if k == nil || k.ClosureSignaler == nil {
		return nil
	}
	return k.ClosureSignaler.CloseChan()
}

func (k *Camera2NDK) closeLocked(ctx context.Context) error {
	var errs []error
	if k.session != nil {
		if err := camera2NDKAbortCaptures(k.session); err != nil &&
			!errors.Is(err, camera.ErrSessionClosed) &&
			!errors.Is(err, camera.ErrCameraDisconnected) {
			errs = append(errs, fmt.Errorf("aborting camera captures: %w", err))
		}
		if err := k.session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing camera session: %w", err))
		}
		k.session = nil
	}
	if !k.sessionClosedForCleanupLocked(ctx, &errs) {
		logger.Debugf(ctx, "deferred Camera2NDK dependent resource cleanup until camera session closes")
		return errors.Join(errs...)
	}
	k.closeSessionDependentResourcesLocked(ctx, &errs)
	return errors.Join(errs...)
}

func (k *Camera2NDK) sessionClosedForCleanupLocked(ctx context.Context, errs *[]error) bool {
	if k.sessionClosed == nil {
		return true
	}
	select {
	case <-k.sessionClosed:
		k.unregisterSessionStateCallbacks()
		k.sessionClosed = nil
		return true
	default:
	}
	if err := k.waitForSessionClosed(ctx, k.sessionClosed); err != nil {
		*errs = append(*errs, fmt.Errorf("waiting for camera session close: %w", err))
		return false
	}
	k.unregisterSessionStateCallbacks()
	k.sessionClosed = nil
	return true
}

func (k *Camera2NDK) closeAfterSessionClosed(ctx context.Context) {
	if k == nil {
		return
	}
	err := xsync.DoA1R1(ctx, &k.Locker, func(ctx context.Context) error {
		var errs []error
		if k.session != nil {
			k.session = nil
		}
		if !k.sessionClosedForCleanupLocked(ctx, &errs) {
			return errors.Join(errs...)
		}
		k.closeSessionDependentResourcesLocked(ctx, &errs)
		return errors.Join(errs...)
	}, ctx)
	if err != nil {
		logger.Debugf(ctx, "deferred Camera2NDK cleanup after camera session close failed: %v", err)
	}
}

func (k *Camera2NDK) closeSessionDependentResourcesLocked(ctx context.Context, errs *[]error) {
	if k.outputs != nil {
		if err := k.outputs.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera output container: %w", err))
		}
		k.outputs = nil
	}
	if k.sessionOutput != nil {
		if err := k.sessionOutput.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera session output: %w", err))
		}
		k.sessionOutput = nil
	}
	if k.outputTarget != nil {
		if err := k.outputTarget.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera output target: %w", err))
		}
		k.outputTarget = nil
	}
	if k.request != nil {
		if err := k.request.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera capture request: %w", err))
		}
		k.request = nil
	}
	if k.nativeWindow != nil {
		if err := k.nativeWindow.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("releasing camera native window: %w", err))
		}
		k.nativeWindow = nil
	}
	k.closeReaderLocked(errs)
	if k.device != nil {
		if err := k.device.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera device: %w", err))
		}
		k.device = nil
	}
	if k.deviceStateCallbackID != 0 {
		cameracapi.BridgeUnregisterDeviceStateCallbacks(k.deviceStateCallbackID)
		k.deviceStateCallbackID = 0
	}
	if k.manager != nil {
		if err := k.manager.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera manager: %w", err))
		}
		k.manager = nil
	}
	logger.Debugf(ctx, "closed Camera2NDK resources")
}

func (k *Camera2NDK) unregisterSessionStateCallbacks() {
	if k.sessionStateCallbackID == 0 {
		return
	}
	cameracapi.BridgeUnregisterSessionStateCallbacks(k.sessionStateCallbackID)
	k.sessionStateCallbackID = 0
}

func (k *Camera2NDK) closeReaderLocked(errs *[]error) {
	k.imageLocker.Lock()
	defer k.imageLocker.Unlock()
	if k.reader != nil {
		if err := k.reader.Close(); err != nil {
			*errs = append(*errs, fmt.Errorf("closing camera image reader: %w", err))
		}
		k.reader = nil
	}
}

func camera2NDKAbortCaptures(session *camera.CaptureSession) error {
	if session == nil {
		return nil
	}
	status := cameracapi.ACameraCaptureSession_abortCaptures(
		(*cameracapi.ACameraCaptureSession)(session.Pointer()),
	)
	if status < 0 {
		return camera.Error(int32(status))
	}
	return nil
}

func camera2NDKOpenCamera(
	mgr *camera.Manager,
	cameraID string,
	callbacks camera.DeviceStateCallbacks,
) (*camera.Device, uintptr, error) {
	callbackID := cameracapi.BridgeRegisterDeviceStateCallbacks(callbacks)
	var callbacksC cameracapi.ACameraDevice_StateCallbacks
	cameracapi.BridgeInitDeviceStateCallbacks(&callbacksC, callbackID)

	var devicePtr *cameracapi.ACameraDevice
	status := cameracapi.ACameraManager_openCamera(
		(*cameracapi.ACameraManager)(mgr.Pointer()),
		cameraID,
		&callbacksC,
		&devicePtr,
	)
	if status < 0 {
		cameracapi.BridgeUnregisterDeviceStateCallbacks(callbackID)
		return nil, 0, camera.Error(int32(status))
	}
	return camera.NewDeviceFromPointer(unsafe.Pointer(devicePtr)), callbackID, nil
}

func camera2NDKCreateCaptureSession(
	dev *camera.Device,
	outputs *camera.SessionOutputContainer,
	callbacks camera.SessionStateCallbacks,
) (*camera.CaptureSession, uintptr, error) {
	callbackID := cameracapi.BridgeRegisterSessionStateCallbacks(callbacks)
	var callbacksC cameracapi.ACameraCaptureSession_stateCallbacks
	cameracapi.BridgeInitSessionStateCallbacks(&callbacksC, callbackID)

	var sessionPtr *cameracapi.ACameraCaptureSession
	status := cameracapi.ACameraDevice_createCaptureSession(
		(*cameracapi.ACameraDevice)(dev.Pointer()),
		(*cameracapi.ACaptureSessionOutputContainer)(outputs.Pointer()),
		&callbacksC,
		&sessionPtr,
	)
	if status < 0 {
		cameracapi.BridgeUnregisterSessionStateCallbacks(callbackID)
		return nil, 0, camera.Error(int32(status))
	}
	return camera.NewCaptureSessionFromPointer(unsafe.Pointer(sessionPtr)), callbackID, nil
}

func camera2NDKAddTarget(
	request *camera.CaptureRequest,
	outputTarget *camera.OutputTarget,
) error {
	status := cameracapi.ACaptureRequest_addTarget(
		(*cameracapi.ACaptureRequest)(request.Pointer()),
		(*cameracapi.ACameraOutputTarget)(outputTarget.Pointer()),
	)
	if status < 0 {
		return fmt.Errorf("adding camera request target: %w", camera.Error(int32(status)))
	}
	return nil
}

func camera2NDKSetRequestFrameRate(
	request *camera.CaptureRequest,
	cfg Camera2NDKConfig,
	metadata Camera2NDKMetadata,
) error {
	targetRange, ok := camera2NDKSelectTargetFPSRange(cfg.FrameRate, metadata.AvailableTargetFPSRanges)
	if !ok {
		return nil
	}
	values := []int32{targetRange.Min, targetRange.Max}
	status := cameracapi.ACaptureRequest_setEntry_i32(
		(*cameracapi.ACaptureRequest)(request.Pointer()),
		uint32(cameracapi.ACAMERA_CONTROL_AE_TARGET_FPS_RANGE),
		uint32(len(values)),
		&values[0],
	)
	if status < 0 {
		return fmt.Errorf("setting camera AE target FPS range: %w", camera.Error(int32(status)))
	}
	return nil
}

func (k *Camera2NDK) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = input
	_ = outputCh
	return kerneltypes.ErrUnexpectedInputType{}
}

func (k *Camera2NDK) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Generate")
	defer func() { logger.Debugf(ctx, "/Generate: %v", _err) }()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	lp := looper.Prepare(int32(looper.ALOOPER_PREPARE_ALLOW_NON_CALLBACKS))
	if lp == nil {
		return fmt.Errorf("unable to prepare Android looper")
	}
	defer func() {
		if err := lp.Close(); err != nil && _err == nil {
			_err = fmt.Errorf("closing Android looper: %w", err)
		}
	}()

	if err := k.open(ctx); err != nil {
		return err
	}
	defer func() {
		if err := k.Close(context.Background()); err != nil {
			_err = errors.Join(_err, err)
		}
	}()

	var firstTimestampNs int64 = -1
	var firstPTS int64 = -1
	var frameIndex int64
	frameDurationNs := int64(camera2NDKTimeBaseDen / max(k.Config.FrameRate, 1))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return io.EOF
		default:
		}

		outFrame, err := k.acquireFrameFromReader(
			ctx,
			&firstTimestampNs,
			&firstPTS,
			frameIndex,
			frameDurationNs,
		)
		switch {
		case errors.Is(err, media.ErrAmediaImgreaderNoBufferAvailable),
			errors.Is(err, media.ErrAmediaImgreaderMaxImagesAcquired),
			errors.Is(err, media.ErrWouldBlock):
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-k.CloseChan():
				return io.EOF
			default:
			}
			looper.PollOnce(camera2NDKLooperPollInterval, nil, nil, nil)
			continue
		case err != nil:
			return err
		}

		frameIndex++

		if err := k.sendFrame(ctx, outputCh, outFrame); err != nil {
			return err
		}
	}
}

func (k *Camera2NDK) acquireFrameFromReader(
	ctx context.Context,
	firstTimestampNs *int64,
	firstPTS *int64,
	frameIndex int64,
	frameDurationNs int64,
) (frame.Output, error) {
	k.imageLocker.Lock()
	defer k.imageLocker.Unlock()

	select {
	case <-ctx.Done():
		return frame.Output{}, ctx.Err()
	case <-k.CloseChan():
		return frame.Output{}, io.EOF
	default:
	}
	if k.reader == nil {
		return frame.Output{}, io.EOF
	}

	image, err := k.reader.AcquireNextImage()
	if err != nil {
		return frame.Output{}, err
	}

	outFrame, err := k.buildFrameFromImage(ctx, image, firstTimestampNs, firstPTS, frameIndex, frameDurationNs)
	closeErr := image.Close()
	if err != nil {
		if closeErr != nil {
			return frame.Output{}, errors.Join(err, fmt.Errorf("closing camera image: %w", closeErr))
		}
		return frame.Output{}, err
	}
	if closeErr != nil {
		frame.Pool.Put(outFrame.Frame)
		return frame.Output{}, fmt.Errorf("closing camera image: %w", closeErr)
	}
	return outFrame, nil
}

func (k *Camera2NDK) open(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &k.Locker, k.openLocked, ctx)
}

func (k *Camera2NDK) openLocked(ctx context.Context) (_err error) {
	if k.reader != nil {
		return fmt.Errorf("camera2 ndk is already open")
	}
	mgr := camera.NewManager()
	if mgr == nil {
		return fmt.Errorf("unable to create camera manager")
	}
	k.manager = mgr
	defer func() {
		if _err != nil {
			_err = errors.Join(_err, k.closeLocked(context.Background()))
		}
	}()

	metadatas, err := camera2NDKReadMetadata(mgr)
	if err != nil {
		return err
	}
	selected, err := resolveCamera2NDKSelection(k.Config, metadatas)
	if err != nil {
		return err
	}
	if err := validateCamera2NDKPhysicalCamera(k.Config, selected); err != nil {
		return err
	}
	if err := validateCamera2NDKStreamFormat(k.Config, selected); err != nil {
		return err
	}

	reader, err := media.NewImageReaderWithUsage(
		k.Config.Width,
		k.Config.Height,
		int32(k.Config.ImageFormat),
		camera2NDKReaderUsageCPURead,
		k.Config.MaxImages,
	)
	if err != nil {
		return fmt.Errorf("creating camera image reader: %w", err)
	}
	k.reader = reader

	window, err := reader.Window()
	if err != nil {
		return fmt.Errorf("getting camera image reader window: %w", err)
	}
	nativeWindow := windowpkg.NewWindowFromPointer(window.Pointer())
	nativeWindow.Acquire()
	k.nativeWindow = nativeWindow
	cameraWindow := (*camera.ANativeWindow)(window.Pointer())

	errCh := make(chan error, 1)
	dev, deviceStateCallbackID, err := camera2NDKOpenCamera(mgr, selected.CameraID, camera.DeviceStateCallbacks{
		OnDisconnected: func() {
			camera2NDKReportDeviceStateError(errCh, ErrCamera2NDKSessionClosed)
			logger.Debugf(ctx, "camera disconnected")
		},
		OnError: func(code int) {
			camera2NDKReportDeviceStateError(
				errCh,
				fmt.Errorf("%w: device error code=%d", ErrCamera2NDKSessionNotReady, code),
			)
			logger.Errorf(ctx, "camera error: %d", code)
		},
	})
	if err != nil {
		return fmt.Errorf("opening camera %s: %w", selected.CameraID, err)
	}
	k.device = dev
	k.deviceStateCallbackID = deviceStateCallbackID

	request, err := dev.CreateCaptureRequest(camera.Record)
	if err != nil {
		return fmt.Errorf("creating camera capture request: %w", err)
	}
	k.request = request
	if err := camera2NDKSetRequestFrameRate(request, k.Config, selected); err != nil {
		return err
	}

	outputTarget, err := camera.NewOutputTarget(cameraWindow)
	if err != nil {
		return fmt.Errorf("creating camera output target: %w", err)
	}
	k.outputTarget = outputTarget
	if err := camera2NDKAddTarget(request, outputTarget); err != nil {
		return err
	}

	sessionOutput, err := camera2NDKNewSessionOutput(cameraWindow, k.Config.PhysicalCameraID)
	if err != nil {
		return err
	}
	k.sessionOutput = sessionOutput

	outputs, err := camera.NewSessionOutputContainer()
	if err != nil {
		return fmt.Errorf("creating camera output container: %w", err)
	}
	k.outputs = outputs
	if err := outputs.Add(sessionOutput); err != nil {
		return fmt.Errorf("adding camera session output: %w", err)
	}

	activeCh := make(chan struct{})
	activeOnce := sync.Once{}
	closedCh := make(chan struct{})
	closedOnce := sync.Once{}
	sessionStateCallbackRef := &atomic.Uintptr{}
	unregisterSessionStateCallbackRef := func() {
		if callbackID := sessionStateCallbackRef.Swap(0); callbackID != 0 {
			cameracapi.BridgeUnregisterSessionStateCallbacks(callbackID)
		}
	}
	session, sessionStateCallbackID, err := camera2NDKCreateCaptureSession(dev, outputs, camera.SessionStateCallbacks{
		OnClosed: func() {
			unregisterSessionStateCallbackRef()
			closedOnce.Do(func() {
				close(closedCh)
			})
			observability.Go(context.Background(), func(ctx context.Context) {
				k.closeAfterSessionClosed(ctx)
			})
			logger.Debugf(ctx, "camera session closed")
		},
		OnReady: func() {
			logger.Debugf(ctx, "camera session ready")
		},
		OnActive: func() {
			activeOnce.Do(func() {
				close(activeCh)
			})
			logger.Debugf(ctx, "camera session active")
		},
	})
	if err != nil {
		return fmt.Errorf("creating camera capture session: %w", err)
	}
	sessionStateCallbackRef.Store(sessionStateCallbackID)
	select {
	case <-closedCh:
		unregisterSessionStateCallbackRef()
	default:
	}
	k.session = session
	k.sessionClosed = closedCh
	k.sessionStateCallbackID = sessionStateCallbackID

	if err := camera2NDKSetRepeatingRequest(session, request); err != nil {
		return fmt.Errorf("setting camera repeating request: %w", err)
	}
	if err := k.waitForSessionActive(ctx, activeCh, closedCh, errCh); err != nil {
		return err
	}
	logger.Debugf(ctx,
		"Camera2NDK capture started: camera_id=%s physical_camera_id=%s format=%s size=%dx%d frame_rate=%d max_images=%d",
		selected.CameraID,
		k.Config.PhysicalCameraID,
		k.Config.ImageFormat,
		k.Config.Width,
		k.Config.Height,
		k.Config.FrameRate,
		k.Config.MaxImages,
	)
	return nil
}

func camera2NDKSetRepeatingRequest(
	session *camera.CaptureSession,
	request *camera.CaptureRequest,
) error {
	requestPtr := (*cameracapi.ACaptureRequest)(request.Pointer())
	var sequenceID int32
	status := cameracapi.ACameraCaptureSession_setRepeatingRequest(
		(*cameracapi.ACameraCaptureSession)(session.Pointer()),
		nil,
		1,
		&requestPtr,
		&sequenceID,
	)
	if status < 0 {
		return camera.Error(int32(status))
	}
	return nil
}

func camera2NDKReportDeviceStateError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}

func (k *Camera2NDK) waitForSessionActive(
	ctx context.Context,
	activeCh <-chan struct{},
	closedCh <-chan struct{},
	errCh <-chan error,
) error {
	timer := time.NewTimer(camera2NDKSessionReadyTimeout)
	defer timer.Stop()

	for {
		select {
		case <-activeCh:
			return nil
		case <-closedCh:
			return ErrCamera2NDKSessionClosed
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-k.CloseChan():
			return ErrCamera2NDKSessionClosed
		case <-timer.C:
			return fmt.Errorf("%w: timeout=%s", ErrCamera2NDKSessionNotReady, camera2NDKSessionReadyTimeout)
		default:
		}
		looper.PollOnce(camera2NDKLooperPollInterval, nil, nil, nil)
	}
}

func (k *Camera2NDK) waitForSessionClosed(
	ctx context.Context,
	closedCh <-chan struct{},
) error {
	if closedCh == nil {
		return nil
	}
	timer := time.NewTimer(camera2NDKSessionReadyTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(camera2NDKLooperPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-closedCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("%w: timeout=%s", ErrCamera2NDKSessionCloseTimeout, camera2NDKSessionReadyTimeout)
		default:
		}
		if lp := looper.ALooper_forThread(); lp != nil && lp.Pointer() != nil {
			looper.PollOnce(camera2NDKLooperPollInterval, nil, nil, nil)
			continue
		}
		select {
		case <-closedCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("%w: timeout=%s", ErrCamera2NDKSessionCloseTimeout, camera2NDKSessionReadyTimeout)
		case <-ticker.C:
		}
	}
}

func camera2NDKReadMetadata(mgr *camera.Manager) ([]Camera2NDKMetadata, error) {
	ids, err := mgr.CameraIDList()
	if err != nil {
		return nil, fmt.Errorf("listing cameras: %w", err)
	}
	metadatas := make([]Camera2NDKMetadata, 0, len(ids))
	for _, id := range ids {
		metadata, err := mgr.GetCameraCharacteristics(id)
		if err != nil {
			return nil, fmt.Errorf("getting camera %s characteristics: %w", id, err)
		}
		converted, convertErr := camera2NDKMetadataFromCharacteristics(id, metadata)
		closeErr := metadata.Close()
		if convertErr != nil {
			return nil, convertErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("closing camera %s metadata: %w", id, closeErr)
		}
		metadatas = append(metadatas, converted)
	}
	return metadatas, nil
}

func camera2NDKNewSessionOutput(
	window *camera.ANativeWindow,
	physicalCameraID string,
) (*camera.SessionOutput, error) {
	if physicalCameraID == "" {
		sessionOutput, err := camera.NewSessionOutput(window)
		if err != nil {
			return nil, fmt.Errorf("creating camera session output: %w", err)
		}
		return sessionOutput, nil
	}
	sessionOutput, err := camera.NewPhysicalSessionOutput(window, physicalCameraID)
	if err != nil {
		return nil, fmt.Errorf("creating physical camera session output: %w", err)
	}
	return sessionOutput, nil
}

func (k *Camera2NDK) buildFrameFromImage(
	ctx context.Context,
	image *media.Image,
	firstTimestampNs *int64,
	firstPTS *int64,
	frameIndex int64,
	frameDurationNs int64,
) (frame.Output, error) {
	_ = ctx
	pixelBytes, err := k.imageBytes(image)
	if err != nil {
		return frame.Output{}, err
	}

	timestampNs, err := camera2NDKImageTimestamp(image)
	if err != nil {
		return frame.Output{}, err
	}
	if *firstTimestampNs < 0 {
		*firstTimestampNs = timestampNs
	}
	if *firstPTS < 0 {
		*firstPTS = initialCamera2NDKPTS()
	}
	pts := camera2NDKPTSFromImageTimestamp(
		*firstPTS,
		*firstTimestampNs,
		timestampNs,
		frameIndex,
		frameDurationNs,
	)

	f := frame.Pool.Get()
	f.Unref()
	f.SetWidth(int(k.Config.Width))
	f.SetHeight(int(k.Config.Height))
	f.SetPixelFormat(k.codecParams.PixelFormat())
	f.SetPts(pts)
	f.SetPktDts(pts)
	f.SetDuration(frameDurationNs)
	if err := f.AllocBuffer(0); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, fmt.Errorf("allocating camera frame buffer: %w", err)
	}
	if err := f.Data().SetBytes(pixelBytes, 1); err != nil {
		frame.Pool.Put(f)
		return frame.Output{}, fmt.Errorf("setting camera frame data: %w", err)
	}
	out := frame.BuildOutput(f, k.streamInfo)
	return out, nil
}

func camera2NDKImageTimestamp(
	image *media.Image,
) (int64, error) {
	var timestampNs int64
	if err := image.Timestamp(&timestampNs); err != nil {
		return 0, fmt.Errorf("reading camera image timestamp: %w", err)
	}
	return timestampNs, nil
}

func (k *Camera2NDK) imageBytes(image *media.Image) ([]byte, error) {
	descriptor, ok := camera2NDKImageFormatDescriptorByFormat(k.Config.ImageFormat)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCamera2NDKUnsupportedImageFormat, k.Config.ImageFormat)
	}
	switch descriptor.PlaneLayout {
	case camera2NDKImagePlaneLayoutYUV420:
		return camera2NDKYUV420ImageBytes(image, int(k.Config.Width), int(k.Config.Height))
	case camera2NDKImagePlaneLayoutRGBA:
		return camera2NDKSinglePlaneImageBytes(image, int(k.Config.Width)*4, int(k.Config.Height))
	default:
		return nil, fmt.Errorf("%w: %s", ErrCamera2NDKUnsupportedImageFormat, k.Config.ImageFormat)
	}
}

func camera2NDKYUV420ImageBytes(
	image *media.Image,
	width int,
	height int,
) ([]byte, error) {
	ySize := width * height
	chromaWidth := (width + 1) / 2
	chromaHeight := (height + 1) / 2
	chromaSize := chromaWidth * chromaHeight
	result := make([]byte, ySize+2*chromaSize)
	if err := camera2NDKCopyPlane(image, 0, width, height, result[:ySize]); err != nil {
		return nil, err
	}
	if err := camera2NDKCopyPlane(image, 1, chromaWidth, chromaHeight, result[ySize:ySize+chromaSize]); err != nil {
		return nil, err
	}
	if err := camera2NDKCopyPlane(image, 2, chromaWidth, chromaHeight, result[ySize+chromaSize:]); err != nil {
		return nil, err
	}
	return result, nil
}

func camera2NDKSinglePlaneImageBytes(
	image *media.Image,
	rowBytes int,
	height int,
) ([]byte, error) {
	result := make([]byte, rowBytes*height)
	dataPtr, dataLength, err := image.PlaneData(0)
	if err != nil {
		return nil, fmt.Errorf("reading camera image plane 0 data: %w", err)
	}
	if dataPtr == nil {
		return nil, fmt.Errorf("camera image plane 0 has nil data")
	}
	var rowStride int32
	if err := image.PlaneRowStride(0, &rowStride); err != nil {
		return nil, fmt.Errorf("reading camera image plane 0 row stride: %w", err)
	}
	if rowStride < int32(rowBytes) {
		return nil, fmt.Errorf("camera image plane 0 row stride too small: row=%d want=%d", rowStride, rowBytes)
	}
	data := unsafe.Slice(dataPtr, int(dataLength))
	for row := 0; row < height; row++ {
		srcStart := row * int(rowStride)
		srcEnd := srcStart + rowBytes
		if srcEnd > len(data) {
			return nil, fmt.Errorf("camera image plane 0 data too short")
		}
		copy(result[row*rowBytes:(row+1)*rowBytes], data[srcStart:srcEnd])
	}
	return result, nil
}

func camera2NDKCopyPlane(
	image *media.Image,
	planeIdx int32,
	width int,
	height int,
	dst []byte,
) error {
	dataPtr, dataLength, err := image.PlaneData(planeIdx)
	if err != nil {
		return fmt.Errorf("reading camera image plane %d data: %w", planeIdx, err)
	}
	if dataPtr == nil {
		return fmt.Errorf("camera image plane %d has nil data", planeIdx)
	}
	var rowStride int32
	if err := image.PlaneRowStride(planeIdx, &rowStride); err != nil {
		return fmt.Errorf("reading camera image plane %d row stride: %w", planeIdx, err)
	}
	var pixelStride int32
	if err := image.PlanePixelStride(planeIdx, &pixelStride); err != nil {
		return fmt.Errorf("reading camera image plane %d pixel stride: %w", planeIdx, err)
	}
	if rowStride <= 0 || pixelStride <= 0 {
		return fmt.Errorf("invalid camera image plane %d strides: row=%d pixel=%d", planeIdx, rowStride, pixelStride)
	}
	data := unsafe.Slice(dataPtr, int(dataLength))
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			srcIdx := row*int(rowStride) + col*int(pixelStride)
			if srcIdx >= len(data) {
				return fmt.Errorf("camera image plane %d data too short", planeIdx)
			}
			dst[row*width+col] = data[srcIdx]
		}
	}
	return nil
}

func (k *Camera2NDK) sendFrame(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
	outFrame frame.Output,
) error {
	select {
	case outputCh <- packetorframe.OutputUnion{Frame: &outFrame}:
		return nil
	case <-ctx.Done():
		frame.Pool.Put(outFrame.Frame)
		return ctx.Err()
	case <-k.CloseChan():
		frame.Pool.Put(outFrame.Frame)
		return io.EOF
	}
}
