package astiav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

func RationalToAstiav(r types.Rational) astiav.Rational {
	return astiav.NewRational(r.Num, r.Den)
}
