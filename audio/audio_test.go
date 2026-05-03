package audio

import (
	"math"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

// TestFillSamplesRoundTrip writes a deterministic, per-channel-distinct
// pattern through FillSamples and reads it back through ExtractSamples.
// It catches the bug where FillSamples wrote into a Go-owned snapshot
// (returned by FrameData.Bytes) instead of the actual frame buffer:
// in that case ExtractSamples returns the AllocBuffer-zeroed memory
// and the InDelta assertions fail on the very first non-zero expected
// sample.
func TestFillSamplesRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		format astiav.SampleFormat
		layout astiav.ChannelLayout
		tol    float64
	}{
		{"Fltp_stereo", astiav.SampleFormatFltp, astiav.ChannelLayoutStereo, 1e-6},
		{"Flt_stereo", astiav.SampleFormatFlt, astiav.ChannelLayoutStereo, 1e-6},
		{"S16P_stereo", astiav.SampleFormatS16P, astiav.ChannelLayoutStereo, 1.0 / 32767.0},
		{"S16_stereo", astiav.SampleFormatS16, astiav.ChannelLayoutStereo, 1.0 / 32767.0},
		{"Dblp_mono", astiav.SampleFormatDblp, astiav.ChannelLayoutMono, 1e-12},
		{"Dbl_mono", astiav.SampleFormatDbl, astiav.ChannelLayoutMono, 1e-12},
	}
	const N = 1024
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := astiav.AllocFrame()
			defer f.Free()
			f.SetSampleFormat(tc.format)
			f.SetChannelLayout(tc.layout)
			f.SetSampleRate(48000)
			f.SetNbSamples(N)
			require.NoError(t, f.AllocBuffer(0))

			nCh := tc.layout.Channels()
			want := make([][]float64, nCh)
			for c := 0; c < nCh; c++ {
				want[c] = make([]float64, N)
				for i := 0; i < N; i++ {
					want[c][i] = 0.5 * math.Sin(2*math.Pi*float64(i+c*7)/float64(N))
				}
				require.NoError(t, FillSamples(f, c, want[c]))
			}
			for c := 0; c < nCh; c++ {
				got, err := ExtractSamples(f, c)
				require.NoError(t, err)
				require.Len(t, got, N)
				for i := 0; i < N; i++ {
					require.InDelta(t, want[c][i], got[i], tc.tol,
						"ch=%d i=%d format=%v", c, i, tc.format)
				}
			}
		})
	}
}
