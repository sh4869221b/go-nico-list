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
		if less, decided := targetIDLess(results[i].ID, results[j].ID); decided {
			return less
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

// targetIDLess compares target IDs using numeric order when both fit uint64.
func targetIDLess(leftID string, rightID string) (bool, bool) {
	left, leftNumeric := normalizedTargetUint64Text(leftID)
	right, rightNumeric := normalizedTargetUint64Text(rightID)
	if leftNumeric && rightNumeric && left != right {
		if len(left) != len(right) {
			return len(left) < len(right), true
		}
		return left < right, true
	}
	if leftNumeric && !rightNumeric {
		return true, true
	}
	if !leftNumeric && rightNumeric {
		return false, true
	}
	return false, false
}

// normalizedTargetUint64Text returns canonical decimal text when text fits uint64.
func normalizedTargetUint64Text(text string) (string, bool) {
	if len(text) == 0 || len(text) > len(maxTargetUint64Text) {
		return text, false
	}
	for i := range text {
		if text[i] < '0' || text[i] > '9' {
			return text, false
		}
	}
	if len(text) == len(maxTargetUint64Text) && text > maxTargetUint64Text {
		return text, false
	}
	for len(text) > 1 && text[0] == '0' {
		text = text[1:]
	}
	return text, true
}

const maxTargetUint64Text = "18446744073709551615"

// normalizeOutputID strips the URL prefix from an output ID.
func normalizeOutputID(id string) string {
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
