package cmd

import (
	"encoding/json/v2"
	"io"
	"sync"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
)

// bufferedOutput collects results for sorted line output and JSON output.
type bufferedOutput struct {
	mu      sync.Mutex
	items   []string
	targets []targetResult
	errors  []string

	// Only the input loop records invalid inputs.
	invalidInputs []string
}

// addTarget records a completed fetch and any partial results it returned.
func (b *bufferedOutput) addTarget(target inputTarget, order int, items []string, err error, jsonOutput bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if jsonOutput {
		result := targetResult{Order: order, Type: target.Type, ID: target.ID, Items: items}
		if err != nil {
			result.Error = err.Error()
			b.errors = append(b.errors, result.Error)
		}
		b.targets = append(b.targets, result)
	}
	b.items = append(b.items, items...)
}

// write formats the collected results after all target workers have finished.
func (b *bufferedOutput) write(out io.Writer, inputs jsonInputs, cfg *RootConfig) outputWriteResult {
	items := b.items
	if cfg.NoSortOutput {
		items = flattenTargetItemsByInputOrder(b.targets)
	}
	if cfg.DedupeOutput && len(items) > 0 {
		items = dedupeItems(items, make(map[string]struct{}, len(items)))
	}
	if len(items) > 0 && !cfg.NoSortOutput {
		niconico.NiconicoSort(items)
	}
	result := outputWriteResult{count: len(items)}
	if cfg.JSONOutput {
		sortTargetResults(b.targets)
		payload := buildJSONOutput(inputs, b.invalidInputs, b.targets, b.errors, items)
		// Encode before writing so invalid strings cannot leave a partial JSON object.
		encoded, err := json.Marshal(payload)
		if err != nil {
			result.err = err
			return result
		}
		_, result.err = out.Write(append(encoded, '\n'))
	} else if len(items) > 0 {
		result.err = writeLineOutput(out, items, cfg.URL)
	}
	return result
}
