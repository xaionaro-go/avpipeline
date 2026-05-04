package fanout

import "github.com/xaionaro-go/avpipeline/preset/selector/id"

type CreationAction uint8

const (
	CreationActionCreate CreationAction = iota + 1
	CreationActionReuse
	CreationActionReject
)

type CreationDecision[K comparable] struct {
	Action        CreationAction
	StorageKey    K
	ReuseMemberID id.MemberID
	RouteIDs      []id.RouteID
}

type ExistingMember[K comparable] struct {
	ID         id.MemberID
	StorageKey K
}
