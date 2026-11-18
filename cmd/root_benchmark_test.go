package cmd

import (
	"fmt"
	"io"
	"testing"
)

func BenchmarkBufferedJSONOutput(b *testing.B) {
	const targetCount = 100
	targets := make([]inputTarget, targetCount)
	batches := make([][]string, targetCount)
	for i := range targets {
		targets[i] = inputTarget{Type: targetTypeUser, ID: fmt.Sprint(i)}
		for j := range 50 {
			batches[i] = append(batches[i], fmt.Sprintf("sm%d", 40000000+j*targetCount+i))
		}
	}
	for _, noSort := range []bool{false, true} {
		b.Run(fmt.Sprintf("noSort=%t", noSort), func(b *testing.B) {
			cfg := RootConfig{JSONOutput: true, NoSortOutput: noSort}
			inputs := jsonInputs{Total: targetCount, Valid: targetCount}
			b.ReportAllocs()
			for b.Loop() {
				var output bufferedOutput
				for i := targetCount - 1; i >= 0; i-- {
					output.addTarget(targets[i], i, batches[i], nil, true)
				}
				result := output.write(io.Discard, inputs, &cfg)
				if result.err != nil || result.count != targetCount*50 {
					b.Fatalf("unexpected output: %+v", result)
				}
			}
		})
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
