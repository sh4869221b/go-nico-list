package niconico

import (
	"slices"
	"testing"
)

func TestNiconicoSort(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "simple",
			input:    []string{"sm12", "sm3", "sm1"},
			expected: []string{"sm1", "sm3", "sm12"},
		},
		{
			name:     "shortString",
			input:    []string{"sm12", "s", "sm3"},
			expected: []string{"sm3", "s", "sm12"},
		},
		{
			name:     "longNumericIDs",
			input:    []string{"sm100000000", "sm99999999", "sm3"},
			expected: []string{"sm3", "sm99999999", "sm100000000"},
		},
		{
			name:     "malformedWithLongNumericIDs",
			input:    []string{"sm100000000", "xx200000000a", "sm99999999"},
			expected: []string{"sm99999999", "sm100000000", "xx200000000a"},
		},
		{
			name:     "paddedNumericIDsLongerThanUint64Text",
			input:    []string{"sm2", "sm0000000000000000000000000000000000000001"},
			expected: []string{"sm0000000000000000000000000000000000000001", "sm2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NiconicoSort(tt.input)
			if !slices.Equal(tt.input, tt.expected) {
				t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, tt.input)
			}
		})
	}
}

func TestNiconicoSortLargeMixedAllocationBudget(t *testing.T) {
	base := mixedSortIDs()

	allocs := testing.AllocsPerRun(20, func() {
		values := append([]string(nil), base...)
		NiconicoSort(values)
	})

	const allocationBudget = 25
	if allocs > allocationBudget {
		t.Fatalf("allocation budget exceeded: got %.0f allocs, want <= %d", allocs, allocationBudget)
	}
}
