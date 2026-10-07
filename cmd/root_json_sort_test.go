package cmd

import (
	"fmt"
	"testing"
)

func TestSortTargetResultsLargeAllocationBudget(t *testing.T) {
	base := largeTargetResults()

	allocs := testing.AllocsPerRun(20, func() {
		results := append([]targetResult(nil), base...)
		sortTargetResults(results)
	})

	const allocationBudget = 25
	if allocs > allocationBudget {
		t.Fatalf("allocation budget exceeded: got %.0f allocs, want <= %d", allocs, allocationBudget)
	}
}

func largeTargetResults() []targetResult {
	base := make([]targetResult, 2000)
	for i := range base {
		targetType := targetTypeUser
		if i%2 == 0 {
			targetType = targetTypeMylist
		}
		id := fmt.Sprintf("%d", 2000-i)
		base[i] = targetResult{Type: targetType, ID: id, Error: fmt.Sprintf("err-%d", i)}
	}

	return base
}
