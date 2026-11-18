package niconico

import (
	"cmp"
	"slices"
)

const maxUint64Text = "18446744073709551615"

// NiconicoSort sorts video IDs by their numeric part in ascending order.
func NiconicoSort(ids []string) {
	// On short inputs the precheck can cost more than it saves.
	if len(ids) >= 32 {
		if eligible, sameWidth := normalVideoIDs(ids); eligible {
			if sameWidth {
				slices.Sort(ids)
			} else {
				slices.SortFunc(ids, compareNormalVideoIDs)
			}
			return
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		left := videoIDSortText(a)
		right := videoIDSortText(b)
		if order := cmp.Compare(len(left), len(right)); order != 0 {
			return order
		}
		if order := cmp.Compare(left, right); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
}

// normalVideoIDs checks for an sm prefix and suffixes that need no normalization.
func normalVideoIDs(ids []string) (eligible, sameWidth bool) {
	width := len(ids[0])
	sameWidth = true
	for _, id := range ids {
		if len(id) < 3 || id[:2] != "sm" || id[2] == '0' {
			return false, false
		}
		if len(id) != width {
			sameWidth = false
		}
	}
	return true, sameWidth
}

// compareNormalVideoIDs orders IDs with a common prefix by width, then text.
func compareNormalVideoIDs(a, b string) int {
	if order := cmp.Compare(len(a), len(b)); order != 0 {
		return order
	}
	return cmp.Compare(a, b)
}

// videoIDSortText normalizes the numeric portion when it fits uint64.
func videoIDSortText(id string) string {
	text := id
	if len(id) >= 2 {
		text = id[2:]
	}
	// Only a leading zero can change the sort key.
	if len(text) < 2 || text[0] != '0' {
		return text
	}
	for i := range text {
		if text[i] < '0' || text[i] > '9' {
			return text
		}
	}
	original := text
	for len(text) > 1 && text[0] == '0' {
		text = text[1:]
	}
	if len(text) > len(maxUint64Text) || len(text) == len(maxUint64Text) && text > maxUint64Text {
		return original
	}
	return text
}
