package niconico

import "sort"

const maxUint64Text = "18446744073709551615"

// videoIDSortKey stores the comparable sort text for a video ID.
type videoIDSortKey struct {
	length int
	text   string
}

// NiconicoSort sorts video IDs by their numeric part in ascending order.
func NiconicoSort(slice []string) {
	sort.Slice(slice, func(i, j int) bool {
		left := videoIDSortKeyFor(slice[i])
		right := videoIDSortKeyFor(slice[j])
		if left.length != right.length {
			return left.length < right.length
		}
		if left.text != right.text {
			return left.text < right.text
		}
		return slice[i] < slice[j]
	})
}

// videoIDSortKeyFor builds an allocation-free key for sorting a video ID.
func videoIDSortKeyFor(id string) videoIDSortKey {
	text := videoIDSortText(id)
	if normalized, ok := normalizedUint64Text(text); ok {
		text = normalized
	}
	return videoIDSortKey{length: len(text), text: text}
}

// normalizedUint64Text returns the canonical decimal text when text fits uint64.
func normalizedUint64Text(text string) (string, bool) {
	if len(text) == 0 || len(text) > len(maxUint64Text) {
		return text, false
	}
	for i := range text {
		if text[i] < '0' || text[i] > '9' {
			return text, false
		}
	}
	if len(text) == len(maxUint64Text) && text > maxUint64Text {
		return text, false
	}
	for len(text) > 1 && text[0] == '0' {
		text = text[1:]
	}
	return text, true
}

// videoIDSortText returns the comparable text portion of a video ID.
func videoIDSortText(id string) string {
	const prefixLen = 2
	if len(id) >= prefixLen {
		return id[prefixLen:]
	}
	return id
}
