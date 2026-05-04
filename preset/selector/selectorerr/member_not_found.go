package selectorerr

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

func MemberNotFound(memberID id.MemberID) error {
	return fmt.Errorf("%w: member %d", ErrMemberNotFound, memberID)
}
