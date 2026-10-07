package cmd

import (
	"context"
	"io"
)

type unorderedWriteResult struct {
	count int
	err   error
}

func writeUnorderedOutput(out io.Writer, outputCh <-chan []string, cfg *RootConfig, cancel context.CancelFunc, done chan<- unorderedWriteResult) {
	var seen map[string]struct{}
	if cfg.DedupeOutput {
		seen = make(map[string]struct{})
	}
	outputCount := 0
	for items := range outputCh {
		if seen != nil {
			items = dedupeItems(items, seen)
		}
		if len(items) > 0 {
			if err := writeLineOutput(out, items, cfg.URL); err != nil {
				cancel()
				done <- unorderedWriteResult{count: outputCount, err: err}
				return
			}
			outputCount += len(items)
		}
	}
	done <- unorderedWriteResult{count: outputCount}
}

func dedupeItems(items []string, seen map[string]struct{}) []string {
	unique := make([]string, 0, len(items))
	for _, id := range items {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
