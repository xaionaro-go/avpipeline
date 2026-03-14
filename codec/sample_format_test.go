// sample_format_test.go provides tests for sample format conversions.

package codec

import (
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
)

func TestSampleFormatFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    astiav.SampleFormat
		wantErr bool
	}{
		{
			name:  "plain u8",
			input: "u8",
			want:  astiav.SampleFormatU8,
		},
		{
			name:  "trimmed float planar",
			input: " fltp ",
			want:  astiav.SampleFormatFltp,
		},
		{
			name:  "uppercase planar",
			input: "S16P",
			want:  astiav.SampleFormatS16P,
		},
		{
			name:    "unsupported",
			input:   "pcm_s24le",
			want:    astiav.SampleFormatNone,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := sampleFormatFromString(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.want, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSampleFormatQuality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format astiav.SampleFormat
		want   int
	}{
		{"dblp highest", astiav.SampleFormatDblp, 6},
		{"dbl", astiav.SampleFormatDbl, 5},
		{"fltp", astiav.SampleFormatFltp, 4},
		{"flt", astiav.SampleFormatFlt, 3},
		{"s32p", astiav.SampleFormatS32P, 2},
		{"s32", astiav.SampleFormatS32, 2},
		{"s16p", astiav.SampleFormatS16P, 1},
		{"s16", astiav.SampleFormatS16, 1},
		{"u8 fallback", astiav.SampleFormatU8, 0},
		{"u8p fallback", astiav.SampleFormatU8P, 0},
		{"none fallback", astiav.SampleFormatNone, 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sampleFormatQuality(tt.format))
		})
	}
}

func TestBestSampleFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		formats []astiav.SampleFormat
		want    astiav.SampleFormat
	}{
		{
			name:    "single format",
			formats: []astiav.SampleFormat{astiav.SampleFormatS16},
			want:    astiav.SampleFormatS16,
		},
		{
			name:    "picks fltp over s16",
			formats: []astiav.SampleFormat{astiav.SampleFormatS16, astiav.SampleFormatFltp},
			want:    astiav.SampleFormatFltp,
		},
		{
			name:    "picks dblp as best from mixed",
			formats: []astiav.SampleFormat{astiav.SampleFormatFlt, astiav.SampleFormatS32, astiav.SampleFormatDblp, astiav.SampleFormatS16},
			want:    astiav.SampleFormatDblp,
		},
		{
			name:    "equal quality picks first",
			formats: []astiav.SampleFormat{astiav.SampleFormatS32, astiav.SampleFormatS32P},
			want:    astiav.SampleFormatS32,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, bestSampleFormat(tt.formats))
		})
	}
}
