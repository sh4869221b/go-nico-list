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

func TestBufferedLineOutputSortsMixedDensityBatches(t *testing.T) {
	for _, dedupe := range []bool{false, true} {
		t.Run(fmt.Sprintf("dedupe=%t", dedupe), func(t *testing.T) {
			var output bufferedOutput
			output.addTarget(inputTarget{}, 0, make([]string, 0, 50), nil, false)
			output.addTarget(inputTarget{}, 1, []string{"sm30", "sm03"}, nil, false)
			output.addTarget(inputTarget{}, 2, []string{"sm10", "sm3", "sm30"}, nil, false)
			sparse := make([]string, 2, 50)
			copy(sparse, []string{"sm2", "sm3"})
			output.addTarget(inputTarget{}, 3, sparse, nil, false)

			var out bytes.Buffer
			result := output.write(&out, jsonInputs{}, &RootConfig{DedupeOutput: dedupe})
			want, count := "sm2\nsm03\nsm3\nsm3\nsm10\nsm30\nsm30\n", 7
			if dedupe {
				want, count = "sm2\nsm03\nsm3\nsm10\nsm30\n", 5
			}
			if result.err != nil || result.count != count || out.String() != want {
				t.Fatalf("output=%q, count=%d, err=%v, want %q (%d items)", out.String(), result.count, result.err, want, count)
			}
		})
	}
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
