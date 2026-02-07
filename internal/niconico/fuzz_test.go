package niconico

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzNiconicoSortNoPanic(f *testing.F) {
	f.Add("sm12\nsm3\nsm1", false, false)
	f.Add(tabStr+urlStr+"sm2\n"+tabStr+urlStr+"sm10\n"+tabStr+urlStr+"sm1", true, true)

	f.Fuzz(func(t *testing.T, raw string, tab bool, url bool) {
		items := strings.Split(raw, "\n")
		if len(items) > 256 {
			items = items[:256]
		}
		values := append([]string(nil), items...)
		NiconicoSort(values, tab, url)
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
