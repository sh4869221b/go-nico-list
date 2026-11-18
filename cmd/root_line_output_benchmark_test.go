package cmd

import (
	"io"
	"strconv"
	"testing"
)

func BenchmarkUnorderedLineOutput(b *testing.B) {
	const targetCount = 100
	for _, tc := range []struct {
		name   string
		videos int
		url    bool
		dedupe bool
	}{
		{name: "SmallTargets", videos: 5},
		{name: "FullPages", videos: 100},
		{name: "URLs", videos: 100, url: true},
		{name: "Deduplicated", videos: 100, dedupe: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			targets := make([][]string, targetCount)
			writableTargets := make([][]string, targetCount)
			stride := tc.videos
			if tc.dedupe {
				stride /= 2
			}
			for i := range targets {
				targets[i] = make([]string, tc.videos)
				if tc.dedupe {
					writableTargets[i] = make([]string, tc.videos)
				}
				for j := range targets[i] {
					targets[i][j] = "sm" + strconv.Itoa(10000000+i*stride+j)
				}
			}
			want := targetCount * tc.videos
			if tc.dedupe {
				want = (targetCount + 1) * tc.videos / 2
			}
			cfg := RootConfig{URL: tc.url, DedupeOutput: tc.dedupe}
			done := make(chan outputWriteResult, 1)
			cancel := func() {}
			b.ReportAllocs()
			for b.Loop() {
				output := make(chan []string, targetCount)
				for i, items := range targets {
					if tc.dedupe {
						copy(writableTargets[i], items)
						items = writableTargets[i]
					}
					output <- items
				}
				close(output)
				writeUnorderedOutput(io.Discard, output, &cfg, cancel, done)
				if result := <-done; result.err != nil || result.count != want {
					b.Fatalf("unexpected write result: %+v", result)
				}
			}
		})
	}
}
