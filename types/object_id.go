package types

import (
	"reflect"
)

// ObjectID is a unique identifier for an object.
type ObjectID uint64

type GetObjectIDer interface {
	GetObjectID() ObjectID
}

type Pointer[T any] interface {
	*T
}

func GetObjectID[P Pointer[T], T any](obj P) ObjectID {
	if obj == nil {
		return ObjectID(0)
	}
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Pointer,
		reflect.Chan,
		reflect.Func,
		reflect.Map,
		reflect.Slice,
		reflect.String,
		reflect.UnsafePointer:
		if v.IsNil() {
			return ObjectID(0)
		}
		ptr := uintptr(v.UnsafePointer())
		if uintptr(uint64(ptr)) != ptr {
			panic("pointer value does not fit into uint64")
		}
		return ObjectID(uint64(ptr))
	}
	panic("reaching this line was supposed to be impossible")
}
