// stream_mux_test.go tests the stream muxer.
package streammux

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/node"
)

func TestStreamMuxNodes(t *testing.T) {
	mux := &StreamMux[struct{}]{}
	v := reflect.ValueOf(mux).Elem()

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
	require.Equal(t, expectedValues, mux.Nodes(context.Background()))
}
