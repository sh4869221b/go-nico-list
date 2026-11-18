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
	inputs jsonInputs,
	invalidInputsList []string,
	targetResults []targetResult,
	errorsList []string,
	outputIDs []string,
) jsonOutputPayload {
	return jsonOutputPayload{
		Inputs:      inputs,
		Invalid:     invalidInputsList,
		Targets:     targetResults,
		Errors:      errorsList,
		OutputCount: len(outputIDs),
		Items:       outputIDs,
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

// flattenTargetItems collects IDs into independent storage for sorting and deduplication.
func flattenTargetItems(results []targetResult, itemCount int, inputOrder bool) []string {
	items := make([]string, 0, itemCount)
	if inputOrder {
		// Every admitted target has one result and a contiguous input position.
		ordered := make([][]string, len(results))
		for _, result := range results {
			ordered[result.Order] = result.Items
		}
		for _, batch := range ordered {
			items = append(items, batch...)
		}
		return items
	}
	for _, result := range results {
		items = append(items, result.Items...)
	}
	return items
}
