package niconico

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func BenchmarkNiconicoSort(b *testing.B) {
	numeric := make([]string, 1000)
	for i := range numeric {
		numeric[i] = fmt.Sprintf("sm%d", 1000-i)
	}
	for _, tc := range []struct {
		name string
		base []string
	}{
		{name: "numeric", base: numeric},
		{name: "mixed", base: mixedSortIDs()},
	} {
		b.Run(tc.name, func(b *testing.B) {
			values := make([]string, len(tc.base))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				copy(values, tc.base)
				b.StartTimer()
				NiconicoSort(values)
			}
		})
	}
}

func mixedSortIDs() []string {
	base := make([]string, 2000)
	for i := range base {
		switch i % 4 {
		case 0:
			base[i] = fmt.Sprintf("sm%d", 2000-i)
		case 1:
			base[i] = fmt.Sprintf("sm%012d", i)
		case 2:
			base[i] = fmt.Sprintf("xx%d", 4000-i)
		default:
			base[i] = fmt.Sprintf("sm%dextra", i)
		}
	}
	return base
}

func BenchmarkGetVideoListLargePayload(b *testing.B) {
	for _, tc := range []struct {
		name, item string
	}{
		{name: "user", item: "essential"},
		{name: "mylist", item: "video"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			fetch := GetVideoList
			prefix, suffix := "", ""
			if tc.name == "mylist" {
				fetch = GetMylistVideoList
				prefix, suffix = `"mylist":{`, "}"
			}
			var payload strings.Builder
			_, _ = fmt.Fprintf(&payload, `{"meta":{"status":200},"data":{%s"items":[`, prefix)
			text := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 4)
			for i := range 100 {
				if i > 0 {
					payload.WriteByte(',')
				}
				_, _ = fmt.Fprintf(&payload, `{"%s":{"id":"sm%d","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12},"title":"%s","description":"%s"}}`, tc.item, i, text, text)
			}
			_, _ = fmt.Fprintf(&payload, `]%s}}`, suffix)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("page") != "1" {
					_, _ = fmt.Fprintf(w, `{"meta":{"status":200},"data":{%s"items":[]%s}}`, prefix, suffix)
					return
				}
				_, _ = io.WriteString(w, payload.String())
			}))
			b.Cleanup(server.Close)
			after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			before := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
			logger := slog.New(slog.DiscardHandler)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ids, err := fetch(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 1, logger, nil)
				if err != nil || len(ids) != 100 {
					b.Fatalf("fetch: %v, ids: %d", err, len(ids))
				}
			}
		})
	}
}
