package niconico

import "sort"

const maxUint64Text = "18446744073709551615"

// NiconicoSort sorts video IDs by their numeric part in ascending order.
func NiconicoSort(slice []string) {
	sort.Slice(slice, func(i, j int) bool {
		left := videoIDSortText(slice[i])
		right := videoIDSortText(slice[j])
		if len(left) != len(right) {
			return len(left) < len(right)
		}
		if left != right {
			return left < right
		}
		return slice[i] < slice[j]
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
