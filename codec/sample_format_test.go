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
