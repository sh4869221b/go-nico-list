package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestNiconicoSort(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		tab      bool
		url      bool
		expected []string
	}{
		{
			name:     "simple",
			input:    []string{"sm12", "sm3", "sm1"},
			tab:      false,
			url:      false,
			expected: []string{"sm1", "sm3", "sm12"},
		},
		{
			name:     "withTab",
			input:    []string{tabStr + "sm12", tabStr + "sm3", tabStr + "sm1"},
			tab:      true,
			url:      false,
			expected: []string{tabStr + "sm1", tabStr + "sm3", tabStr + "sm12"},
		},
		{
			name:     "withTabAndURL",
			input:    []string{tabStr + urlStr + "sm2", tabStr + urlStr + "sm10", tabStr + urlStr + "sm1"},
			tab:      true,
			url:      true,
			expected: []string{tabStr + urlStr + "sm1", tabStr + urlStr + "sm2", tabStr + urlStr + "sm10"},
		},
		{
			name:     "shortString",
			input:    []string{"sm12", "s", "sm3"},
			tab:      false,
			url:      false,
			expected: []string{"sm3", "s", "sm12"},
		},
		{
			name:     "shortStringTabURL",
			input:    []string{tabStr + urlStr + "sm2", tabStr + urlStr + "s", tabStr + urlStr + "sm10"},
			tab:      true,
			url:      true,
			expected: []string{tabStr + urlStr + "s", tabStr + urlStr + "sm2", tabStr + urlStr + "sm10"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			slice := append([]string(nil), tt.input...)
			NiconicoSort(slice, tt.tab, tt.url)
			if !reflect.DeepEqual(slice, tt.expected) {
				t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, slice)
			}
		})
	}
}

func TestRetriesRequest(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	res, err := retriesRequest(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if count != 3 {
		t.Errorf("expected 3 attempts, got %d", count)
	}
}

func TestRetriesRequestBackoff(t *testing.T) {
	var calls []time.Time
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, time.Now())
		count++
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	res, err := retriesRequest(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	diff1 := calls[1].Sub(calls[0])
	diff2 := calls[2].Sub(calls[1])
	if diff1 < 100*time.Millisecond {
		t.Errorf("first backoff too short: %v", diff1)
	}
	if diff2 < 200*time.Millisecond {
		t.Errorf("second backoff too short: %v", diff2)
	}
}

func TestRetriesRequestContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := retriesRequest(ctx, "http://example.com")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if res != nil {
		t.Errorf("expected nil response, got %v", res)
	}
}

func TestRetriesRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldTimeout := httpClientTimeout
	httpClientTimeout = 50 * time.Millisecond
	defer func() { httpClientTimeout = oldTimeout }()

	start := time.Now()
	res, err := retriesRequest(context.Background(), server.URL)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if res != nil {
		t.Errorf("expected nil response, got %v", res)
	}
	if elapsed < httpClientTimeout {
		t.Errorf("timeout returned too quickly: %v", elapsed)
	}
}

func TestGetVideoList(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var resp string
		switch page {
		case "1":
			resp = `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-15T00:00:00Z","count":{"comment":3}}}]}}`
		case "2":
			resp = `{"data":{"items":[{"essential":{"id":"sm3","registeredAt":"2024-02-10T00:00:00Z","count":{"comment":20}}},{"essential":{"id":"sm4","registeredAt":"2024-05-02T00:00:00Z","count":{"comment":30}}}]}}`
		default:
			resp = `{"data":{"items":[]}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	oldLimit := pageLimit
	pageLimit = 3
	defer func() { pageLimit = oldLimit }()

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got := getVideoList("12345", 5, after, before, false, false, server.URL)
	expected := []string{"sm1", "sm3"}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}
