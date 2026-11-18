package cmd

import (
	"bufio"
	"context"
	"io"
)

func writeUnorderedOutput(out io.Writer, outputCh <-chan []string, cfg *RootConfig, cancel context.CancelFunc, done chan<- outputWriteResult) {
	var seen map[string]struct{}
	if cfg.DedupeOutput {
		seen = make(map[string]struct{})
	}
	outputCount := 0
	var writer *bufio.Writer
	for items := range outputCh {
		if seen != nil {
			// Each completed fetch transfers ownership of its items to this writer.
			items = dedupeItems(items, seen)
		}
		if len(items) > 0 {
			if writer == nil {
				writer = bufio.NewWriter(out)
			}
			if err := writeLineBatch(writer, items, cfg.URL); err != nil {
				cancel()
				done <- outputWriteResult{count: outputCount, err: err}
				return
			}
			outputCount += len(items)
		}
	}
	done <- outputWriteResult{count: outputCount}
}

// dedupeItems compacts items in place, preserving the order of first occurrences.
func dedupeItems(items []string, seen map[string]struct{}) []string {
	unique := items[:0]
	for _, id := range items {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
