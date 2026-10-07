package cmd

import (
	"sort"
	"strings"
)

const nicoWatchURLPrefix = "https://www.nicovideo.jp/watch/"

// jsonInputs summarizes input counts for JSON output.
type jsonInputs struct {
	Total   int64 `json:"total"`
	Valid   int64 `json:"valid"`
	Invalid int64 `json:"invalid"`
}

// targetResult captures per-input-target results for JSON output.
type targetResult struct {
	Order int      `json:"-"`
	Type  string   `json:"type"`
	ID    string   `json:"id"`
	Items []string `json:"items"`
	Error string   `json:"error"`
}

// jsonOutputPayload defines the JSON output schema.
type jsonOutputPayload struct {
	Inputs      jsonInputs     `json:"inputs"`
	Invalid     []string       `json:"invalid"`
	Targets     []targetResult `json:"targets"`
	Errors      []string       `json:"errors"`
	OutputCount int            `json:"output_count"`
	Items       []string       `json:"items"`
}

// buildJSONOutput assembles the JSON payload from run results.
func buildJSONOutput(
	totalInputs int64,
	validInputs int64,
	invalidInputs int64,
	invalidInputsList []string,
	targetResults []targetResult,
	errorsList []string,
	outputCount int,
	outputIDs []string,
) jsonOutputPayload {
	targets := make([]targetResult, 0, len(targetResults))
	for _, target := range targetResults {
		targets = append(targets, targetResult{
			Type:  target.Type,
			ID:    target.ID,
			Items: normalizeOutputList(target.Items),
			Error: target.Error,
		})
	}
	return jsonOutputPayload{
		Inputs: jsonInputs{
			Total:   totalInputs,
			Valid:   validInputs,
			Invalid: invalidInputs,
		},
		Invalid:     append([]string{}, invalidInputsList...),
		Targets:     targets,
		Errors:      append([]string{}, errorsList...),
		OutputCount: outputCount,
		Items:       normalizeOutputList(outputIDs),
	}
}

// sortTargetResults sorts target results by type and numeric id in ascending order.
func sortTargetResults(results []targetResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		// Parsed target IDs are decimal strings of at most twelve digits.
		left := strings.TrimLeft(results[i].ID, "0")
		right := strings.TrimLeft(results[j].ID, "0")
		if len(left) != len(right) {
			return len(left) < len(right)
		}
		if left != right {
			return left < right
		}
		if results[i].ID != results[j].ID {
			return results[i].ID < results[j].ID
		}
		return results[i].Error < results[j].Error
	})
}

func flattenTargetItemsByInputOrder(results []targetResult) []string {
	ordered := append([]targetResult{}, results...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Order < ordered[j].Order
	})
	outputIDs := make([]string, 0)
	for _, result := range ordered {
		outputIDs = append(outputIDs, result.Items...)
	}
	return outputIDs
}

// normalizeOutputList normalizes a list of output IDs.
func normalizeOutputList(items []string) []string {
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, strings.TrimPrefix(item, nicoWatchURLPrefix))
	}
	return normalized
}
