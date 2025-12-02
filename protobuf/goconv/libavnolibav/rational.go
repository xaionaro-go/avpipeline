package libavnolibav

import (
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

type Rational libav_proto.Rational

func RationalFromProtobuf(input *libav_proto.Rational) *Rational {
	return (*Rational)(input)
}

func (f *Rational) Protobuf() *libav_proto.Rational {
	return (*libav_proto.Rational)(f)
}
