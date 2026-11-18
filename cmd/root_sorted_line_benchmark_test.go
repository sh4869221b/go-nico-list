package cmd

import (
	"io"
	"strconv"
	"testing"
)

func BenchmarkSortedLineOutput(b *testing.B) {
	for _, tc := range []struct {
		name          string
		targets       int
		batchSize     int
		batchCapacity int
		stride        int
		dedupe        bool
	}{
		{name: "OneTarget", targets: 1, batchSize: 10000, batchCapacity: 10000, stride: 10000},
		{name: "OneDenseTarget", targets: 1, batchSize: 10000, batchCapacity: 14000, stride: 10000},
		{name: "ThreeTargets", targets: 3, batchSize: 500, batchCapacity: 500, stride: 500},
		{name: "DenseTargets", targets: 100, batchSize: 100, batchCapacity: 100, stride: 100},
		{name: "FilteredTargets", targets: 100, batchSize: 5, batchCapacity: 100, stride: 5},
		{name: "DenseDedupe", targets: 100, batchSize: 100, batchCapacity: 100, stride: 100, dedupe: true},
		{name: "HalfDuplicate", targets: 100, batchSize: 100, batchCapacity: 100, stride: 50, dedupe: true},
		{name: "HighlyDuplicate", targets: 100, batchSize: 100, batchCapacity: 100, stride: 0, dedupe: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			batches := make([][]string, tc.targets)
			first := make([]string, tc.batchSize, tc.batchCapacity)
			for i := range batches {
				batches[i] = make([]string, tc.batchSize, tc.batchCapacity)
				// IDs tend to descend within a fetched target, while workers
				// finish in a different order across targets.
				target := i * 37 % tc.targets
				for j := range batches[i] {
					batches[i][j] = "sm" + strconv.Itoa(41000000+target*tc.stride+tc.batchSize-j)
				}
			}
			want := tc.batchSize * tc.targets
			if tc.dedupe {
				want = tc.batchSize + (tc.targets-1)*tc.stride
			}
			cfg := RootConfig{DedupeOutput: tc.dedupe}
			b.ReportAllocs()
			for b.Loop() {
				var output bufferedOutput
				copy(first, batches[0])
				output.addTarget(inputTarget{}, 0, first, nil, false)
				for i, batch := range batches[1:] {
					output.addTarget(inputTarget{}, i+1, batch, nil, false)
				}
				result := output.write(io.Discard, jsonInputs{}, &cfg)
				if result.err != nil || result.count != want {
					b.Fatalf("unexpected output: %+v", result)
				}
			}
		})
	}
}
