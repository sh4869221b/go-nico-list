package cmd

import "strings"

const (
	targetTypeUser   = "user"
	targetTypeMylist = "mylist"
)

type inputTarget struct {
	Type string
	ID   string
}

func parseInputTarget(input string) (inputTarget, bool) {
	// Schemes and host prefixes are optional, so search for the bare paths.
	if id := findTargetID(input, "nicovideo.jp/user/", 9); id != "" {
		return inputTarget{Type: targetTypeUser, ID: id}, true
	}
	if id := findTargetID(input, "nicovideo.jp/mylist/", 12); id != "" {
		return inputTarget{Type: targetTypeMylist, ID: id}, true
	}
	return inputTarget{}, false
}

// findTargetID searches embedded target paths and returns the first ASCII ID.
func findTargetID(input, path string, maxDigits int) string {
	for {
		_, rest, found := strings.Cut(input, path)
		if !found {
			return ""
		}
		end := 0
		limit := min(len(rest), maxDigits)
		for end < limit && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end > 0 {
			return rest[:end]
		}
		input = rest
	}
}
