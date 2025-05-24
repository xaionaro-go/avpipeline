package router

import (
	"fmt"
)

type PublishMode int

const (
	PublishModeExclusiveTakeover = PublishMode(iota)
	PublishModeExclusiveFail
	PublishModeSharedTakeover
	PublishModeSharedFail
	EndOfPublishMode
)

func (m PublishMode) String() string {
	switch m {
	case PublishModeExclusiveTakeover:
		return "exclusive-takeover"
	case PublishModeExclusiveFail:
		return "exclusive-fail"
	case PublishModeSharedTakeover:
		return "exclusive-shared-takeover"
	case PublishModeSharedFail:
		return "exclusive-shared-fail"
	default:
		return fmt.Sprintf("<unknown_mode_%d>", int(m))
	}
}

func (m PublishMode) IsExclusive() bool {
	switch m {
	case PublishModeExclusiveTakeover:
		return true
	case PublishModeExclusiveFail:
		return true
	}
	return false
}

func (m PublishMode) FailOnConflict() bool {
	switch m {
	case PublishModeExclusiveFail:
		return true
	case PublishModeSharedFail:
		return true
	}
	return false
}
