package niconico

import (
	"cmp"
	"fmt"
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

func TestNiconicoSortSameWidthAndMixedIDs(t *testing.T) {
	base := make([]string, 100)
	for i := range base {
		base[i] = fmt.Sprintf("sm%d", 40000000+(i*43)%len(base))
	}
	for _, tc := range []struct {
		name    string
		outlier string
	}{
		{name: "same width"},
		{name: "padded", outlier: "sm00000042"},
		{name: "mixed prefix", outlier: "xx30000000"},
		{name: "mixed width", outlier: "sm9999999"},
		{name: "malformed same width", outlier: "sm5abc89xy"},
		{name: "malformed zero prefix", outlier: "sm0abc89xy"},
		{name: "invalid byte same width", outlier: "sm4\xff\x0067890"},
		{name: "invalid byte mixed width", outlier: "sm\xff\x00"},
		{name: "short", outlier: "s"},
		{name: "prefix only", outlier: "sm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := slices.Clone(base)
			if tc.outlier != "" {
				input[len(input)-2] = tc.outlier
			}
			want := slices.Clone(input)
			slices.SortFunc(want, func(a, b string) int {
				left, right := videoIDSortText(a), videoIDSortText(b)
				if order := cmp.Compare(len(left), len(right)); order != 0 {
					return order
				}
				if order := cmp.Compare(left, right); order != 0 {
					return order
				}
				return cmp.Compare(a, b)
			})
			NiconicoSort(input)
			for i := range input {
				if input[i] != want[i] {
					t.Fatalf("index %d: got %q, want %q", i, input[i], want[i])
				}
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
