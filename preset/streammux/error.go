// error.go defines error types for the stream muxer.

package streammux

import (
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type ErrSwitchAlreadyInProgress struct {
	OutputIDCurrent OutputID
	OutputIDNext    OutputID
}

func (e ErrSwitchAlreadyInProgress) Error() string {
	return fmt.Sprintf("switch already in progress: %d -> %d", e.OutputIDCurrent, e.OutputIDNext)
}

type ErrOutputAlreadyPreferred struct {
	OutputID OutputID
}

func (e ErrOutputAlreadyPreferred) Error() string {
	return fmt.Sprintf("output %v is already preferred", e.OutputID)
}

type ErrOutputsAlreadyPreferred struct {
	OutputIDs []OutputID
}

func (e ErrOutputsAlreadyPreferred) Error() string {
	return fmt.Sprintf("outputs %v is already preferred", e.OutputIDs)
}

type ErrStop struct{}

func (e ErrStop) Error() string {
	return "stopped"
}

type ErrUnsupportedCodec struct {
	CodecID astiav.CodecID
}

func (e ErrUnsupportedCodec) Error() string {
	return fmt.Sprintf("unsupported codec: %v", e.CodecID)
}

type ErrNoResolutionsSpecified struct{}

func (e ErrNoResolutionsSpecified) Error() string {
	return "at least one resolution must be specified for automatic bitrate control"
}

type ErrUnableToGetConnectionInfo struct {
	Processor interface{}
	Err       error
}

func (e ErrUnableToGetConnectionInfo) Error() string {
	return fmt.Sprintf("unable to get connection info from %T: %v", e.Processor, e.Err)
}

func (e ErrUnableToGetConnectionInfo) Unwrap() error {
	return e.Err
}

type ErrOutputDifferentNotAllowed struct {
	MuxMode types.MuxMode
}

func (e ErrOutputDifferentNotAllowed) Error() string {
	return fmt.Sprintf("changing output is not allowed in the current MuxMode: %v", e.MuxMode)
}

type ErrUnableToDisableBypass struct {
	Err error
}

func (e ErrUnableToDisableBypass) Error() string {
	return fmt.Sprintf("unable to disable bypass mode: %v", e.Err)
}

func (e ErrUnableToDisableBypass) Unwrap() error {
	return e.Err
}

type ErrUnableToEnableBypass struct {
	Err error
}

func (e ErrUnableToEnableBypass) Error() string {
	return fmt.Sprintf("unable to enable bypass mode: %v", e.Err)
}

func (e ErrUnableToEnableBypass) Unwrap() error {
	return e.Err
}

type ErrUnableToChangeResolution struct {
	Err error
}

func (e ErrUnableToChangeResolution) Error() string {
	return fmt.Sprintf("unable to change resolution: %v", e.Err)
}

func (e ErrUnableToChangeResolution) Unwrap() error {
	return e.Err
}

type ErrUnableToGetCurrentResolution struct {
	Err error
}

func (e ErrUnableToGetCurrentResolution) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("unable to get current resolution: %v", e.Err)
	}
	return "unable to get current resolution"
}

func (e ErrUnableToGetCurrentResolution) Unwrap() error {
	return e.Err
}

type ErrNoResolutionConfigFound struct {
	Resolution codec.Resolution
}

func (e ErrNoResolutionConfigFound) Error() string {
	return fmt.Sprintf("unable to find a resolution config for the current resolution %v", e.Resolution)
}

type ErrUnableToSetBitrate struct {
	Bitrate types.Ubps
	Err     error
}

func (e ErrUnableToSetBitrate) Error() string {
	return fmt.Sprintf("unable to set bitrate to %v: %v", e.Bitrate, e.Err)
}

func (e ErrUnableToSetBitrate) Unwrap() error {
	return e.Err
}

type ErrUnableToSetNewResolution struct {
	Resolution codec.Resolution
	Err        error
}

func (e ErrUnableToSetNewResolution) Error() string {
	return fmt.Sprintf("unable to set new resolution %v: %v", e.Resolution, e.Err)
}

func (e ErrUnableToSetNewResolution) Unwrap() error {
	return e.Err
}

type ErrResolutionAlreadySet struct {
	Key types.SenderKey
}

func (e ErrResolutionAlreadySet) Error() string {
	return fmt.Sprintf("output is already set to %v", e.Key)
}

type ErrUnableToCompareOutputKeys struct {
	Key1 types.SenderKey
	Key2 types.SenderKey
}

func (e ErrUnableToCompareOutputKeys) Error() string {
	return fmt.Sprintf("unable to compare output keys: %v and %v", e.Key1, e.Key2)
}

type ErrUnableToEnableVideoTranscodingBypass struct {
	Err error
}

func (e ErrUnableToEnableVideoTranscodingBypass) Error() string {
	return fmt.Sprintf("unable to enable video transcoding bypass: %v", e.Err)
}

func (e ErrUnableToEnableVideoTranscodingBypass) Unwrap() error {
	return e.Err
}

type ErrUnableToSetResolution struct {
	Resolution codec.Resolution
	Err        error
}

func (e ErrUnableToSetResolution) Error() string {
	return fmt.Sprintf("unable to set resolution to %v: %v", e.Resolution, e.Err)
}

func (e ErrUnableToSetResolution) Unwrap() error {
	return e.Err
}
