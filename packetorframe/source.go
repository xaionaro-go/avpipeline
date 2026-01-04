// source.go defines the abstract source interface for packets and frames.

package packetorframe

import (
	"fmt"
)

type AbstractSource interface {
	fmt.Stringer
}
