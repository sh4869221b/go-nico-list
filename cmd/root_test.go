package cmd

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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
	}

	for _, tt := range tests {
		slice := append([]string(nil), tt.input...)
		NiconicoSort(slice, tt.tab, tt.url)
		if !reflect.DeepEqual(slice, tt.expected) {
			t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, slice)
		}
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

	res, err := retriesRequest(server.URL)
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
