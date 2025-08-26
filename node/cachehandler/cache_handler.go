package cachehandler

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type CachePolicy int

const (
	UndefinedCachePolicy CachePolicy = iota
	CachePolicyDisabled
	CachePolicySinceLastKeyFrame
	EndOfCachePolicy
)

type CacheHandler struct {
	Policy         CachePolicy
	packetCache    []packet.Input
	locker         xsync.Mutex
	needsSendingTo xsync.Map[node.Abstract, struct{}]
}

var _ node.CachingHandler = (*CacheHandler)(nil)

func New(
	cachePolicy CachePolicy,
	maxCachedPackets int,
) *CacheHandler {
	return &CacheHandler{
		Policy:      cachePolicy,
		packetCache: make([]packet.Input, 0, maxCachedPackets),
	}
}

func (h *CacheHandler) RememberPacketIfNeeded(
	ctx context.Context,
	pkt packet.Input,
) (_err error) {
	mediaType := pkt.GetMediaType()
	logger.Tracef(ctx, "RememberPacketIfNeeded: pts:%d mediaType:%s", pkt.Pts(), mediaType)
	defer func() {
		logger.Tracef(ctx, "/RememberPacketIfNeeded: pts:%d mediaType:%s: %v", pkt.Pts(), mediaType, _err)
	}()

	if h.Policy == CachePolicyDisabled {
		return nil
	}
	return xsync.DoR1(ctx, &h.locker, func() error {
		switch h.Policy {
		case CachePolicyDisabled:
			return nil
		case CachePolicySinceLastKeyFrame:
			return h.handlePacketSinceLastKeyFrame(ctx, pkt)
		default:
			return fmt.Errorf("unknown cache policy: %v", h.Policy)
		}
	})
}

func (h *CacheHandler) handlePacketSinceLastKeyFrame(
	ctx context.Context,
	pkt packet.Input,
) (_err error) {
	mediaType := pkt.GetMediaType()
	logger.Tracef(ctx, "handlePacketSinceLastKeyFrame: pts:%d mediaType:%s", pkt.Pts(), mediaType)
	defer func() {
		logger.Tracef(ctx, "/handlePacketSinceLastKeyFrame: pts:%d mediaType:%s: %v", pkt.Pts(), mediaType, _err)
	}()

	if mediaType != astiav.MediaTypeVideo {
		return nil
	}

	if pkt.Flags().Has(astiav.PacketFlagKey) {
		panic("DO NOT USE ME; for some reason the stream gets corrupted; haven't understood why, yet")
		for _, p := range h.packetCache {
			packet.Pool.Put(p.Packet)
		}
		h.packetCache = h.packetCache[:0]
		p := pkt.Packet
		logger.Tracef(ctx, "keyframe detected, cache cleared: %d %v %v %v %v %v %v %v", len(p.Data()), p.Dts(), p.Pts(), p.Duration(), p.Pos(), p.Size(), p.Flags(), p.TimeBase())
	}

	if len(h.packetCache) >= cap(h.packetCache) {
		return fmt.Errorf("packet cache is full (%d packets)", cap(h.packetCache))
	}

	h.packetCache = append(h.packetCache, packet.BuildInput(
		packet.CloneAsReferenced(pkt.Packet),
		pkt.StreamInfo,
	))
	logger.Tracef(ctx, "cached packet pts:%d mediaType:%s, total cached packets: %d", pkt.Pts(), mediaType, len(h.packetCache))
	return nil
}

func (h *CacheHandler) OnAddPushPacketsTo(
	ctx context.Context,
	pushTo node.PushPacketsTo,
) {
	logger.Debugf(ctx, "OnAddPushPacketsTo: %s", pushTo.Node)
	h.needsSendingTo.Store(pushTo.Node, struct{}{})
}
func (h *CacheHandler) OnRemovePushPacketsTo(
	ctx context.Context,
	pushTo node.PushPacketsTo,
) {
	logger.Debugf(ctx, "OnRemovePushPacketsTo: %s", pushTo.Node)
	h.needsSendingTo.Delete(pushTo.Node)
}

func (h *CacheHandler) GetPendingPackets(
	ctx context.Context,
	pushTo node.PushPacketsTo,
) (_ret []packet.Input, _err error) {
	logger.Tracef(ctx, "GetPendingPackets")
	defer func() { logger.Tracef(ctx, "/GetPendingPackets: %d %v", len(_ret), _err) }()

	if _, ok := h.needsSendingTo.LoadAndDelete(pushTo.Node); !ok {
		return nil, nil
	}

	defer func() { logger.Debugf(ctx, "sending %d cached packets to %s", len(_ret), pushTo.Node) }()

	h.locker.Do(ctx, func() {
		_ret = make([]packet.Input, 0, len(h.packetCache))
		for _, pkt := range h.packetCache {
			_ret = append(_ret, packet.BuildInput(
				packet.CloneAsReferenced(pkt.Packet),
				pkt.StreamInfo,
			))
		}
	})
	return
}

func (h *CacheHandler) Reset(
	ctx context.Context,
) {
	logger.Debugf(ctx, "Reset")
	defer func() { logger.Debugf(ctx, "/Reset: %v") }()
	h.locker.Do(ctx, func() {
		for _, p := range h.packetCache {
			packet.Pool.Put(p.Packet)
		}
		h.packetCache = h.packetCache[:0]
		h.needsSendingTo.Range(func(key node.Abstract, value struct{}) bool {
			h.needsSendingTo.Delete(key)
			return true
		})
	})
}
