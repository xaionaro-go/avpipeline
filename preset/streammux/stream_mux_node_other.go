//go:build !unix
// +build !unix

package streammux

import (
	"context"
)

func processPlatformSpecificError(
	ctx context.Context,
	err error,
) error {
	return err
}
