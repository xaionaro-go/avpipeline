// data_has_prefix.go implements a condition that checks if packet data has a specific prefix.

package condition

import (
	"bytes"
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type DataHasPrefix []byte

var _ Condition = (DataHasPrefix)(nil)

func (v DataHasPrefix) String() string {
	return fmt.Sprintf("DataHasPrefix(%X)", []byte(v))
}

func (v DataHasPrefix) Match(
	_ context.Context,
	input packet.Input,
) bool {
	return bytes.HasPrefix(input.Data(), v)
}
