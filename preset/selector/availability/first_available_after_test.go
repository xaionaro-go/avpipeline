package availability

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	available bool
}

func (s fakeSource) HasResources(context.Context) bool {
	return s.available
}

func TestFirstAvailableAfter(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		members    []Candidate
		after      int
		expected   int
		expectedOK bool
	}{
		{
			name: "absent candidate skipped",
			members: []Candidate{
				Present(AlwaysAvailable{}),
				Absent(),
				Present(AlwaysAvailable{}),
			},
			after:      0,
			expected:   2,
			expectedOK: true,
		},
		{
			name: "nil source present means available",
			members: []Candidate{
				Present(AlwaysAvailable{}),
				Present(nil),
			},
			after:      0,
			expected:   1,
			expectedOK: true,
		},
		{
			name: "unavailable source skipped",
			members: []Candidate{
				Present(AlwaysAvailable{}),
				Present(fakeSource{available: false}),
				Present(fakeSource{available: true}),
			},
			after:      0,
			expected:   2,
			expectedOK: true,
		},
		{
			name: "after before first starts at zero",
			members: []Candidate{
				Present(fakeSource{available: true}),
			},
			after:      -4,
			expected:   0,
			expectedOK: true,
		},
		{
			name: "out of range after returns no candidate",
			members: []Candidate{
				Present(fakeSource{available: true}),
			},
			after:      4,
			expected:   0,
			expectedOK: false,
		},
		{
			name: "maximum positive after returns no candidate",
			members: []Candidate{
				Present(fakeSource{available: true}),
			},
			after:      math.MaxInt,
			expected:   0,
			expectedOK: false,
		},
		{
			name: "after at last index returns no candidate",
			members: []Candidate{
				Present(fakeSource{available: true}),
			},
			after:      0,
			expected:   0,
			expectedOK: false,
		},
		{
			name: "all unavailable returns no candidate",
			members: []Candidate{
				Present(fakeSource{available: true}),
				Present(fakeSource{available: false}),
				Absent(),
				Present(fakeSource{available: false}),
			},
			after:      0,
			expected:   0,
			expectedOK: false,
		},
		{
			name: "sparse fallback jumps over absent slots",
			members: []Candidate{
				Present(fakeSource{available: true}),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Absent(),
				Present(fakeSource{available: true}),
			},
			after:      0,
			expected:   10,
			expectedOK: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := FirstAvailableAfter(ctx, test.members, test.after)
			require.Equal(t, test.expectedOK, ok)
			require.Equal(t, test.expected, actual)
		})
	}
}
