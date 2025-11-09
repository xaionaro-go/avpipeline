package streammux

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type dummyHandler struct {
	boilerplate.CustomHandler
}

func (dummyHandler) String() string {
	return "dummyHandler"
}

type dummyOutputFactory struct{}

func (dummyOutputFactory) NewSender(
	ctx context.Context,
	outputKey SenderKey,
) (SendingNode, types.SenderConfig, error) {
	return node.NewWithCustomDataFromKernel[OutputCustomData](
		ctx,
		boilerplate.NewKernelWithFormatContext(ctx, &dummyHandler{}),
	), types.SenderConfig{}, nil
}

func TestOutputNodes(t *testing.T) {
	ctx := context.Background()
	output, err := newOutput[struct{}](
		ctx,
		1,
		newInputNode[struct{}](ctx, nil),
		dummyOutputFactory{},
		SenderKey{
			Resolution: codectypes.Resolution{Width: 1920, Height: 1080},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	v := reflect.ValueOf(output).Elem()

	var expectedValues []node.Abstract
	for i := range v.NumField() {
		fT := v.Type().Field(i)
		fTT := fT.Type
		if !fTT.Implements(reflect.TypeOf((*node.Abstract)(nil)).Elem()) {
			continue
		}

		fV := v.Field(i)
		expectedValues = append(expectedValues, fV.Interface().(node.Abstract))
	}

	// we have zero outputs, so there be only the global streammux nodes:
	require.Equal(t, expectedValues, output.Nodes())
}
