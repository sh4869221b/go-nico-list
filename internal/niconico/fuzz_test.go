package niconico

import (
	"encoding/json"
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
		values := append([]string(nil), items...)
		NiconicoSort(values)
	})
}

func FuzzNicoDataUnmarshalNoPanic(f *testing.F) {
	f.Add([]byte(`{"meta":{"status":200},"data":{"totalCount":0,"items":[]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, b []byte) {
		var payload NicoData
		_ = json.Unmarshal(b, &payload)
	})
}
