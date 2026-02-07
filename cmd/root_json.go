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

// userResult captures per-user results for JSON output.
type userResult struct {
	UserID string   `json:"user_id"`
	Items  []string `json:"items"`
	Error  string   `json:"error"`
}

// jsonOutputPayload defines the JSON output schema.
type jsonOutputPayload struct {
	Inputs      jsonInputs   `json:"inputs"`
	Invalid     []string     `json:"invalid"`
	Users       []userResult `json:"users"`
	Errors      []string     `json:"errors"`
	OutputCount int          `json:"output_count"`
	Items       []string     `json:"items"`
}

// buildJSONOutput assembles the JSON payload from run results.
func buildJSONOutput(
	totalInputs int64,
	validInputs int64,
	invalidInputs int64,
	invalidInputsList []string,
	userResults []userResult,
	errorsList []string,
	outputCount int,
	outputIDs []string,
) jsonOutputPayload {
	items := make([]string, 0, len(outputIDs))
	for _, id := range outputIDs {
		items = append(items, normalizeOutputID(id))
	}
	users := make([]userResult, 0, len(userResults))
	for _, user := range userResults {
		users = append(users, userResult{
			UserID: user.UserID,
			Items:  normalizeOutputList(user.Items),
			Error:  user.Error,
		})
	}
	return jsonOutputPayload{
		Inputs: jsonInputs{
			Total:   totalInputs,
			Valid:   validInputs,
			Invalid: invalidInputs,
		},
		Invalid:     append([]string{}, invalidInputsList...),
		Users:       users,
		Errors:      append([]string{}, errorsList...),
		OutputCount: outputCount,
		Items:       items,
	}
}

// sortUserResultsByUserID sorts user results by numeric user_id in ascending order.
func sortUserResultsByUserID(results []userResult) {
	sort.Slice(results, func(i, j int) bool {
		leftID, leftErr := strconv.Atoi(results[i].UserID)
		rightID, rightErr := strconv.Atoi(results[j].UserID)
		if leftErr == nil && rightErr == nil && leftID != rightID {
			return leftID < rightID
		}
		if leftErr == nil && rightErr != nil {
			return true
		}
		if leftErr != nil && rightErr == nil {
			return false
		}
		if results[i].UserID != results[j].UserID {
			return results[i].UserID < results[j].UserID
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
