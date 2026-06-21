package niconico

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestGetVideoListPageConcurrencyIgnoresErrorAfterEmptyPage(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	page3Started := make(chan struct{})
	var page3StartedOnce sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			select {
			case <-page3Started:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		case "3":
			page3StartedOnce.Do(func() { close(page3Started) })
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "invalid")
		default:
			t.Errorf("unexpected page request: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Cleanup(func() { page3StartedOnce.Do(func() { close(page3Started) }) })

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 2, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1"}) {
		t.Fatalf("unexpected ids: %v", got)
	}
}

func TestGetVideoListPageConcurrencyReturnsEarlierPageBeforeLaterEmptyPage(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	releasePage2 := make(chan struct{})
	var releasePage2Once sync.Once
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
			releasePage2Once.Do(func() { close(releasePage2) })
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		default:
			t.Errorf("unexpected page request: %s", r.URL.Query().Get("page"))
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Cleanup(func() { releasePage2Once.Do(func() { close(releasePage2) }) })

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 2, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1", "sm2"}) {
		t.Fatalf("unexpected ids: %v", got)
	}
}

func TestGetVideoListPageConcurrencyCancelsToEmptyResult(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	page2Started := make(chan struct{})
	var page2StartedOnce sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			page2StartedOnce.Do(func() { close(page2Started) })
			<-r.Context().Done()
		default:
			select {
			case <-r.Context().Done():
			default:
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			}
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)
	resultCh := make(chan struct {
		ids []string
		err error
	}, 1)
	go func() {
		ids, err := GetVideoList(ctx, "12345", 0, after, before, server.URL, 1, time.Second, nil, 2, logger)
		resultCh <- struct {
			ids []string
			err error
		}{ids: ids, err: err}
	}()
	select {
	case <-page2Started:
	case <-time.After(time.Second):
		t.Fatal("expected page 2 request to start")
	}
	cancel()
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("unexpected error: %v", result.err)
		}
		if len(result.ids) != 0 {
			t.Fatalf("expected empty result after cancellation, got %v", result.ids)
		}
	case <-time.After(time.Second):
		t.Fatal("expected canceled fetch to finish")
	}
}
