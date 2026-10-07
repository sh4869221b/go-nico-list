package cmd

import (
	"fmt"
	"testing"
)

func BenchmarkBuildJSONOutputLarge(b *testing.B) {
	targets := make([]targetResult, 100)
	outputIDs := make([]string, 0, 5000)
	for i := range targets {
		items := make([]string, 50)
		for j := range items {
			items[j] = fmt.Sprintf("sm%d", i*50+j)
		}
		targets[i] = targetResult{Type: targetTypeUser, ID: fmt.Sprintf("%d", i), Items: items}
		outputIDs = append(outputIDs, items...)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		payload := buildJSONOutput(100, 100, 0, nil, targets, nil, len(outputIDs), outputIDs)
		if payload.OutputCount != len(outputIDs) {
			b.Fatalf("unexpected output_count: %d", payload.OutputCount)
		}
	}
}

func BenchmarkSortTargetResultsLarge(b *testing.B) {
	base := largeTargetResults()
	results := make([]targetResult, len(base))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(results, base)
		sortTargetResults(results)
	}
}

func executeBenchmarkRootCommand(cfg RootConfig, deps RootDeps, args ...string) error {
	cmd := NewRootCommand(cfg, deps)
	cmd.SetArgs(args)
	return cmd.Execute()
}
