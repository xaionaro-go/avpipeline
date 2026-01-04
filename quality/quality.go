// quality.go defines the interface for quality settings.

// Package quality provides tools for managing quality settings.
package quality

import (
	"fmt"
	"reflect"

	"github.com/asticode/go-astiav"
)

type Quality interface {
	typeName() string
	Apply(*astiav.CodecParameters) error
}

type valueSetter interface {
	setValues(vq qualitySerializable) error
}

type qualitySerializable map[string]any

func (vq qualitySerializable) typeName() string {
	result, _ := vq["type"].(string)
	return result
}

func (vq qualitySerializable) setValues(in qualitySerializable) error {
	for k := range vq {
		delete(vq, k)
	}
	for k, v := range in {
		vq[k] = v
	}
	return nil
}

func (vq qualitySerializable) Convert() (Quality, error) {
	typeName, ok := vq["type"].(string)
	if !ok {
		return nil, fmt.Errorf("field 'type' is not set")
	}

	var r Quality
	for _, sample := range []Quality{
		ptr(ConstantBitrate(0)),
		ptr(ConstantQuality(0)),
	} {
		if sample.typeName() == typeName {
			r = sample
			break
		}
	}
	if r == nil {
		return nil, fmt.Errorf("unknown type '%s'", typeName)
	}

	if err := r.(valueSetter).setValues(vq); err != nil {
		return nil, fmt.Errorf("unable to convert the value: %w", err)
	}
	return reflect.ValueOf(r).Elem().Interface().(Quality), nil
}
