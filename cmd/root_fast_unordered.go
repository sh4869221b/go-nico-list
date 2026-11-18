package cmd

import (
	"context"
	"io"
)

func writeUnorderedOutput(out io.Writer, outputCh <-chan []string, cfg *RootConfig, cancel context.CancelFunc) outputWriteResult {
	var seen map[string]struct{}
	if cfg.DedupeOutput {
		seen = make(map[string]struct{})
	}
	outputCount := 0
	var writer lineOutputWriter
	for items := range outputCh {
		if seen != nil {
			// Each completed fetch transfers ownership of its items to this writer.
			items = dedupeItems(items, seen)
		}
		if len(items) > 0 {
			if writer.buffer == nil {
				writer = newLineOutputWriter(out)
			}
			result := writer.writeBatch(items, cfg.URL)
			outputCount += result.count
			if result.err != nil {
				cancel()
				return outputWriteResult{count: outputCount, err: result.err}
			}
		}
	}
	return outputWriteResult{count: outputCount}
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
