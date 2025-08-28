package avpipeline

import (
	"context"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
)

func Drain(
	ctx context.Context,
	setBlockInput *bool,
	nodes ...node.Abstract,
) (_err error) {
	logger.Tracef(ctx, "Drain")
	defer func() { logger.Tracef(ctx, "/Drain: %v", _err) }()
	return Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		n node.Abstract,
	) (_err error) {
		logger.Debugf(ctx, "draining %v", n)
		defer func() { logger.Debugf(ctx, "/draining %v: %v", n, _err) }()
		if setBlockInput != nil {
			node.SetBlockInput(ctx, *setBlockInput, n)
		}
		if err := n.Flush(ctx); err != nil {
			return fmt.Errorf("unable to flush internal buffers of %v: %w", n, err)
		}
		for {
			ch := n.GetChangeChanDrained()
			if n.IsDrained(ctx) {
				return nil
			}
			logger.Debugf(ctx, "waiting for drain of %v", n)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ch:
			}
		}
	}, nodes...)
}

func IsDrained(
	ctx context.Context,
	nodes ...node.Abstract,
) (_ret bool) {
	logger.Tracef(ctx, "IsDrained")
	defer func() { logger.Tracef(ctx, "/IsDrained: %v", _ret) }()
	_ret = true
	Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		n node.Abstract,
	) (_err error) {
		if !n.IsDrained(ctx) {
			logger.Tracef(ctx, "node %v:%p is not drained", n, n)
			_ret = false
			return ErrTraverseStop{}
		}
		return nil
	}, nodes...)
	return
}

func WaitForDrain(
	ctx context.Context,
	nodes ...node.Abstract,
) (_err error) {
	logger.Tracef(ctx, "WaitForDrain")
	defer func() { logger.Tracef(ctx, "/WaitForDrain: %v", _err) }()
	return Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		n node.Abstract,
	) (_err error) {
		logger.Debugf(ctx, "waiting for drain of %v", n)
		defer func() { logger.Debugf(ctx, "/waiting for drain of %v: %v", n, _err) }()
		return node.WaitForDrain(ctx, n)
	}, nodes...)
}

func SetBlockInput(
	ctx context.Context,
	blocked bool,
	nodes ...node.Abstract,
) error {
	return Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		n node.Abstract,
	) error {
		node.SetBlockInput(ctx, blocked, n)
		return nil
	}, nodes...)
}
