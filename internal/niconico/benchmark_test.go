package niconico

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkNiconicoSort(b *testing.B) {
	base := make([]string, 1000)
	for i := range base {
		base[i] = fmt.Sprintf("sm%d", 1000-i)
	}
	values := make([]string, len(base))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		copy(values, base)
		b.StartTimer()
		NiconicoSort(values)
	}
}

func BenchmarkNiconicoSortLargeMixed(b *testing.B) {
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
	values := make([]string, len(base))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		copy(values, base)
		b.StartTimer()
		NiconicoSort(values)
	}
}

func BenchmarkGetVideoListLargeUserPayload(b *testing.B) {
	payload := largeUserPayload(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, payload)
	}))
	b.Cleanup(server.Close)
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.DiscardHandler)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ids, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 1, logger)
		if err != nil {
			b.Fatalf("GetVideoList returned error: %v", err)
		}
		if len(ids) != 100 {
			b.Fatalf("expected 100 ids, got %d", len(ids))
		}
	}
}

func BenchmarkGetMylistVideoListLargePayload(b *testing.B) {
	payload := largeMylistPayload(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[]}}}`)
			return
		}
		_, _ = io.WriteString(w, payload)
	}))
	b.Cleanup(server.Close)
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.DiscardHandler)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ids, err := GetMylistVideoList(context.Background(), "847130", 0, after, before, server.URL, 1, time.Second, nil, 1, logger)
		if err != nil {
			b.Fatalf("GetMylistVideoList returned error: %v", err)
		}
		if len(ids) != 100 {
			b.Fatalf("expected 100 ids, got %d", len(ids))
		}
	}
}

func largeUserPayload(count int) string {
	payload := `{"meta":{"status":200},"data":{"items":[`
	for i := range count {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf(`{"essential":{"id":"sm%d","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12},"title":"%s","description":"%s"}}`, i, benchmarkText, benchmarkText)
	}
	return payload + `]}}`
}

func largeMylistPayload(count int) string {
	payload := `{"meta":{"status":200},"data":{"mylist":{"items":[`
	for i := range count {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf(`{"video":{"id":"sm%d","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12},"title":"%s","description":"%s"}}`, i, benchmarkText, benchmarkText)
	}
	return payload + `]}}}`
}

const benchmarkText = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
