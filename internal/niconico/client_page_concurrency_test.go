package niconico

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestGetVideoListSequentialNilItemsStopsPagination(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	var requestedPages []string
	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{}}`)
		default:
			t.Errorf("unexpected page request: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, 1, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1"}) {
		t.Fatalf("unexpected ids: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requestedPages, []string{"1", "2"}) {
		t.Fatalf("unexpected requested pages: %v", requestedPages)
	}
}

func TestGetVideoListPageConcurrencyPreservesPageOrder(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	releasePage2 := make(chan struct{})
	var releaseOnce sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			select {
			case <-releasePage2:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
		case "3":
			releaseOnce.Do(func() { close(releasePage2) })
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm3","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
		default:
			t.Errorf("unexpected page request: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releasePage2) }) })

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, 2, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1", "sm2", "sm3"}) {
		t.Fatalf("expected page-order ids, got %v", got)
	}
}

func TestGetVideoListPageConcurrencyStopsSchedulingAfterFetchError(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	var requestedPages []string
	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":500,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "invalid")
		case "3", "4":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		default:
			t.Errorf("unexpected page request after error: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, 2, logger)
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if !reflect.DeepEqual(got, []string{"sm1"}) {
		t.Fatalf("unexpected partial ids: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Contains(requestedPages, "5") {
		t.Fatalf("unexpected requested pages: %v", requestedPages)
	}
}

func TestGetMylistVideoListPageConcurrencyUsesTotalItemCount(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	var requestedPages []string
	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"totalItemCount":200,"items":[{"video":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}}`)
		case "2":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[{"video":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}}`)
		default:
			t.Errorf("unexpected page request: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[]}}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetMylistVideoList(context.Background(), "847130", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, 2, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1", "sm2"}) {
		t.Fatalf("unexpected ids: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requestedPages, []string{"1", "2"}) {
		t.Fatalf("unexpected requested pages: %v", requestedPages)
	}
}
