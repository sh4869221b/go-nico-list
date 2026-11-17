package niconico

import (
	"cmp"
	"slices"
)

const maxUint64Text = "18446744073709551615"

// NiconicoSort sorts video IDs by their numeric part in ascending order.
func NiconicoSort(ids []string) {
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

// videoIDSortText normalizes the numeric portion when it fits uint64.
func videoIDSortText(id string) string {
	text := id
	if len(id) >= 2 {
		text = id[2:]
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
