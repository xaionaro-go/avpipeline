package eviction

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
)

func (h *Handler[K, M]) findSibling(
	ctx context.Context,
	routeID id.RouteID,
	deadID id.MemberID,
	liveMembers map[id.MemberID]member.Entry[K, M],
) (member.Entry[K, M], bool) {
	siblingID, ok := h.attachments.FirstSibling(ctx, routeID, deadID, func(candidate id.MemberID) bool {
		_, alive := liveMembers[candidate]
		return alive
	})
	if !ok {
		return member.Entry[K, M]{}, false
	}

	entry, ok := liveMembers[siblingID]
	return entry, ok
}
