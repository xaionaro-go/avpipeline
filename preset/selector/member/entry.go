package member

import "github.com/xaionaro-go/avpipeline/preset/selector/id"

type Entry[K comparable, M any] struct {
	ID         id.MemberID
	StorageKey K
	Value      M
	Token      Token
}

func sameEntry[K comparable, M any](
	left Entry[K, M],
	right Entry[K, M],
) bool {
	return left.ID == right.ID &&
		left.StorageKey == right.StorageKey &&
		left.Token == right.Token
}
