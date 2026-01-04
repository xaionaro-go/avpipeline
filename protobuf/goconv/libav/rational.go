// rational.go provides conversion functions for rational numbers between Protobuf and Go.

package libav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv/libavnolibav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Rational = libavnolibav.Rational

func RationalFromProtobuf(input *libav_proto.Rational) *Rational {
	return libavnolibav.RationalFromProtobuf(input)
}

func RationalFromGo(input *astiav.Rational) *Rational {
	if input == nil {
		return nil
	}
	return &Rational{
		N: int64(input.Num()),
		D: int64(input.Den()),
	}
}

func RationalToGo(f *Rational) *astiav.Rational {
	if f == nil {
		return nil
	}
	return ptr(astiav.NewRational(int(f.N), int(f.D)))
}
