package cmd

import (
	"sort"
	"strconv"
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
	items := make([]string, 0, len(outputIDs))
	for _, id := range outputIDs {
		items = append(items, normalizeOutputID(id))
	}
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
		Items:       items,
	}
}

// sortTargetResults sorts target results by type and numeric id in ascending order.
func sortTargetResults(results []targetResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		leftID, leftErr := strconv.ParseUint(results[i].ID, 10, 64)
		rightID, rightErr := strconv.ParseUint(results[j].ID, 10, 64)
		if leftErr == nil && rightErr == nil && leftID != rightID {
			return leftID < rightID
		}
		if leftErr == nil && rightErr != nil {
			return true
		}
		if leftErr != nil && rightErr == nil {
			return false
		}
		if results[i].ID != results[j].ID {
			return results[i].ID < results[j].ID
		}
		return results[i].Error < results[j].Error
	})
}

// normalizeOutputID strips tab and URL prefixes from an output ID.
func normalizeOutputID(id string) string {
	id = strings.TrimLeft(id, "\t")
	return strings.TrimPrefix(id, nicoWatchURLPrefix)
}

// normalizeOutputList normalizes a list of output IDs.
func normalizeOutputList(items []string) []string {
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeOutputID(item))
	}
	return normalized
}
