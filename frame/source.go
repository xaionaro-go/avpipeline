// source.go defines the Source interface for media frames.

package frame

import (
	"fmt"
)

type Source interface {
	fmt.Stringer
}
