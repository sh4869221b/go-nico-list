package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteLineOutputBatchesWrites(t *testing.T) {
	var out countingWriter
	items := make([]string, 100)
	for i := range items {
		items[i] = fmt.Sprintf("sm%d", i)
	}

	if err := writeLineOutput(&out, items, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.writes > 2 {
		t.Fatalf("expected batched writes, got %d writes", out.writes)
	}
}

type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

func TestRunRootCmdSortOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ids := []string{"sm2", "sm1"}
		if r.URL.Query().Get("page") != "1" {
			ids = nil
		}
		_, _ = io.WriteString(w, httpCommandPagePayload(false, 0, ids))
	}))
	t.Cleanup(server.Close)
	for _, noSort := range []bool{false, true} {
		t.Run(fmt.Sprintf("no_sort=%t", noSort), func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.NoSortOutput = noSort
			out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "sm1\nsm2\n"
			if noSort {
				want = "sm2\nsm1\n"
			}
			if got := out.String(); got != want {
				t.Fatalf("output=%q, want %q", got, want)
			}
		})
	}
}
