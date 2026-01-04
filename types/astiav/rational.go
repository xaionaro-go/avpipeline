// rational.go provides conversion functions between types.Rational and astiav.Rational.

package astiav

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/types"
)

func RationalToAstiav(r types.Rational) astiav.Rational {
	return astiav.NewRational(r.Num, r.Den)
}
