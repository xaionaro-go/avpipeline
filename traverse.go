package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
)

type ErrTraverseStop struct{}

func (ErrTraverseStop) Error() string { return "traverse: stop requested" }

func Traverse[T node.Abstract](
	ctx context.Context,
	callback func(ctx context.Context, parent node.Abstract, item reflect.Type, node node.Abstract) error,
	nodes ...T,
) (_err error) {
	logger.Debugf(ctx, "Traverse: %v", nodes)
	defer func() {
		logger.Debugf(ctx, "/Traverse: %v: %v", nodes, _err)
	}()

	alreadyVisited := map[node.Abstract]struct{}{}
	_, err := traverse(ctx, alreadyVisited, callback, nil, nil, nodes...)
	return err
}

func traverse[T node.Abstract](
	ctx context.Context,
	alreadyVisited map[node.Abstract]struct{},
	callback func(ctx context.Context, parent node.Abstract, item reflect.Type, node node.Abstract) error,
	parent node.Abstract,
	itemType reflect.Type,
	nodes ...T,
) (_continue bool, _err error) {
	logger.Debugf(ctx, "current nodes len: %d", len(nodes))
	var errs []error
	for _, n := range nodes {
		_, isVisited := alreadyVisited[n]
		logger.Tracef(ctx, "node: %s; is_visited: %t", n, isVisited)
		if isVisited {
			continue
		}
		alreadyVisited[n] = struct{}{}

		nextNodes, err := nextLayer(n)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to get the next layer: %w", err))
		}
		logger.Debugf(ctx, "next layer len: %d", len(nextNodes))

		proc := n.GetProcessor()
		if proc == nil {
			errs = append(errs, fmt.Errorf("the Processor is nil in node %v", n))
		} else {
			err := callback(ctx, parent, itemType, n)
			switch {
			case err == nil:
			case err == ErrTraverseStop{}:
				return false, errors.Join(errs...)
			default:
				errs = append(errs, fmt.Errorf("unable to close '%v': %w", n, err))
			}
		}

		for _, r := range nextNodes {
			var shouldContinue bool
			shouldContinue, err = traverse(ctx, alreadyVisited, callback, parent, r.ItemType, r.Node)
			if err != nil {
				errs = append(errs, fmt.Errorf("unable to close children of '%v': %w", n, err))
			}
			if !shouldContinue {
				return false, errors.Join(errs...)
			}
		}
	}

	return true, errors.Join(errs...)
}
