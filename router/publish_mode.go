package router

import (
	"fmt"
)

type PublishMode int

const (
	UndefinedPublishMode PublishMode = PublishMode(iota)
	PublishModeExclusiveTakeover
	PublishModeExclusiveFail
	PublishModeSharedTakeover
	PublishModeSharedFail
	EndOfPublishMode
)

func (m PublishMode) String() string {
	switch m {
	case UndefinedPublishMode:
		return "<undefined>"
	case PublishModeExclusiveTakeover:
		return "exclusive-takeover"
	case PublishModeExclusiveFail:
		return "exclusive-fail"
	case PublishModeSharedTakeover:
		return "shared-takeover"
	case PublishModeSharedFail:
		return "shared-fail"
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
