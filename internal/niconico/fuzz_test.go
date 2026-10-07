package niconico

import (
	"strings"
	"testing"
)

func FuzzNiconicoSortNoPanic(f *testing.F) {
	f.Add("sm12\nsm3\nsm1")
	f.Add("sm2\nsm10\nsm1")

	f.Fuzz(func(t *testing.T, raw string) {
		items := strings.Split(raw, "\n")
		if len(items) > 256 {
			items = items[:256]
		}
		NiconicoSort(items)
	})
}
