package cmd

import (
	"encoding/json/v2"
	"io"
	"sync"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
)

// bufferedOutput collects results for sorted line output and JSON output.
type bufferedOutput struct {
	mu        sync.Mutex
	itemCount int
	items     []string
	batches   [][]string
	targets   []targetResult
	errors    []string

	// Only the input loop records invalid inputs.
	invalidInputs []string
}

// addTarget records a completed fetch and any partial results it returned.
func (b *bufferedOutput) addTarget(target inputTarget, order int, items []string, err error, jsonOutput bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.itemCount += len(items)
	if !jsonOutput {
		if len(items) == 0 {
			return
		}
		dense := len(items) >= cap(items)-len(items)
		switch {
		case dense && b.items == nil:
			b.items = items
		case dense && len(items) > cap(b.items)-len(b.items):
			// Save dense batches for one final copy. Sparse batches would retain
			// too much of their backing arrays, so copy them into items now.
			b.batches = append(b.batches, items)
		default:
			b.items = append(b.items, items...)
		}
		return
	}
	result := targetResult{Order: order, Type: target.Type, ID: target.ID, Items: items}
	if err != nil {
		result.Error = err.Error()
		b.errors = append(b.errors, result.Error)
	}
	b.targets = append(b.targets, result)
}

// write formats the collected results after all target workers have finished.
func (b *bufferedOutput) write(out io.Writer, inputs jsonInputs, cfg *RootConfig) outputWriteResult {
	items := b.items
	if cfg.JSONOutput {
		items = flattenTargetItems(b.targets, b.itemCount, cfg.NoSortOutput)
	} else if len(b.batches) > 0 {
		// Buffered line output is always sorted, so sparse and dense
		// batches can be accumulated separately.
		items = append(make([]string, 0, b.itemCount), items...)
		for _, batch := range b.batches {
			items = append(items, batch...)
		}
		b.items = items
		b.batches = nil
	}
	if cfg.DedupeOutput && len(items) > 0 {
		items = dedupeItems(items, make(map[string]struct{}, len(items)))
	}
	if len(items) > 0 && !cfg.NoSortOutput {
		niconico.NiconicoSort(items)
	}
	if cfg.JSONOutput {
		sortTargetResults(b.targets)
		payload := buildJSONOutput(inputs, b.invalidInputs, b.targets, b.errors, items)
		// Encode before writing so invalid strings cannot leave a partial JSON object.
		encoded, err := json.Marshal(payload)
		if err != nil {
			return outputWriteResult{err: err}
		}
		n, err := out.Write(append(encoded, '\n'))
		if n < len(encoded)+1 && err == nil {
			err = io.ErrShortWrite
		}
		written, err := flushOutput(out, int64(n), err)
		result := outputWriteResult{err: err}
		if written >= int64(len(encoded)) {
			result.count = len(items)
		}
		return result
	} else if len(items) > 0 {
		return newLineOutputWriter(out).writeBatch(items, cfg.URL)
	}
	return outputWriteResult{}
}
