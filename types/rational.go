package types

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	dectofrac "github.com/av-elier/go-decimal-to-rational"
)

type Rational struct {
	Num int
	Den int
}

func newNTSCRationalFromFloat64(f float64) *big.Rat {
	den := 1001 // common denominator for NTSC frame rates
	num := math.Ceil(f) * 1000
	r := big.NewRat(int64(num), int64(den))
	confirmValue, _ := r.Float64()
	if math.Abs(f-confirmValue) < 1e-2 {
		return r
	}
	return nil
}

func RationalFromApproxFloat64(fps float64) Rational {
	var r Rational
	if float64(int(fps)) == fps {
		r.Num = int(fps)
		r.Den = 1
		return r
	}
	rat := newNTSCRationalFromFloat64(fps)
	if rat == nil {
		for _, order := range []float64{1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6} {
			rat = dectofrac.NewRatP(fps, order)
			if v, _ := rat.Float64(); v != math.Round(fps) {
				break
			}
		}
	}
	r.Num = int(rat.Num().Int64())
	r.Den = int(rat.Denom().Int64())
	return r
}

func RationalFromFloat64(fps float64) Rational {
	var r Rational
	if float64(int(fps)) == fps {
		r.Num = int(fps)
		r.Den = 1
		return r
	}
	rat := dectofrac.NewRatP(fps, 1e-6)
	r.Num = int(rat.Num().Int64())
	r.Den = int(rat.Denom().Int64())
	return r
}

func RationalFromString(s string) (*Rational, error) {
	var r Rational
	switch {
	case len(s) == 0:
		return nil, fmt.Errorf("unable to parse Rational from empty string")
	case strings.Contains(s, "/"):
		if _, err := fmt.Sscanf(s, "%d/%d", &r.Num, &r.Den); err != nil {
			return nil, fmt.Errorf("unable to parse Rational from %q: %w", s, err)
		}
	case s[0] == '~':
		fps, err := strconv.ParseFloat(s[1:], 64)
		if err != nil {
			return nil, fmt.Errorf("unable to parse Rational from %q: %w", s, err)
		}
		r = RationalFromApproxFloat64(fps)
	default:
		fps, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("unable to parse Rational from %q: %w", s, err)
		}
		r = RationalFromFloat64(fps)
	}
	if r.Den == 0 {
		return nil, fmt.Errorf("denominator cannot be zero")
	}
	return &r, nil
}

func (r Rational) Float64() float64 {
	return float64(r.Num) / float64(r.Den)
}

func (r Rational) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Rational) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unable to unmarshal Rational from JSON '%s': %w", b, err)
	}
	v, err := RationalFromString(s)
	if err != nil {
		return fmt.Errorf("unable to unmarshal Rational from string %q: %w", s, err)
	}
	*r = *v
	return nil
}

func (r Rational) String() string {
	return fmt.Sprintf("%d/%d", r.Num, r.Den)
}

func (r *Rational) UnmarshalYAML(b []byte) error {
	return r.UnmarshalJSON(b)
}

func (r Rational) MarshalYAML() ([]byte, error) {
	return r.MarshalJSON()
}
