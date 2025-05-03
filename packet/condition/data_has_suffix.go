package condition

import (
	"bytes"
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type DataHasSuffix []byte

var _ Condition = (DataHasSuffix)(nil)

func (v DataHasSuffix) String() string {
	return fmt.Sprintf("DataHasSuffix(%X)", []byte(v))
}

func (v DataHasSuffix) Match(
	_ context.Context,
	input packet.Input,
) bool {
	return bytes.HasPrefix(input.Data(), v)
}
