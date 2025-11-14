package goconv

import (
	"github.com/asticode/go-astiav"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Rational libav_proto.Rational

func RationalFromProtobuf(input *libav_proto.Rational) *Rational {
	return (*Rational)(input)
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

func (f *Rational) Protobuf() *libav_proto.Rational {
	return (*libav_proto.Rational)(f)
}

func (f *Rational) Go() *astiav.Rational {
	if f == nil {
		return nil
	}
	return ptr(astiav.NewRational(int(f.N), int(f.D)))
}
