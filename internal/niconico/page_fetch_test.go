package niconico

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real transport, including net/http's transparent decompression.
func TestFetchPageBodyLimitHTTP(t *testing.T) {
	for _, endpoint := range []struct {
		name  string
		parse parsePageFunc
	}{
		{"user", parseUserVideoPage}, {"mylist", parseMylistPage},
	} {
		for _, transfer := range []string{"length", "chunked", "gzip"} {
			for _, size := range []int{maxPageBodyBytes - 1, maxPageBodyBytes, maxPageBodyBytes + 1} {
				t.Run(endpoint.name+"/"+transfer+"/"+strconv.Itoa(size), func(t *testing.T) {
					payload := `{"meta":{"status":200},"data":{"items":[]}}`
					body := payload + strings.Repeat(" ", size-len(payload))
					var requests atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						switch transfer {
						case "length":
							w.Header().Set("Content-Length", strconv.Itoa(len(body)))
							_, _ = io.WriteString(w, body)
						case "chunked":
							w.(http.Flusher).Flush()
							_, _ = io.WriteString(w, body)
						case "gzip":
							if r.Header.Get("Accept-Encoding") != "gzip" {
								t.Error("transport did not request gzip")
							}
							var compressed bytes.Buffer
							zw := gzip.NewWriter(&compressed)
							_, _ = io.WriteString(zw, body)
							_ = zw.Close()
							w.Header().Set("Content-Encoding", "gzip")
							w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
							_, _ = w.Write(compressed.Bytes())
						}
					}))
					defer server.Close()
					parsed := false
					parse := func(b []byte) (parsedPage, error) { parsed = true; return endpoint.parse(b) }
					control := NewHTTPControl(1, true)
					_, err := fetchPage(context.Background(), server.URL, 10*time.Second, 3, nil, slog.New(slog.DiscardHandler), parse, control)
					oversized := size > maxPageBodyBytes
					if oversized && !errors.Is(err, errPageBodyTooLarge) || !oversized && err != nil {
						t.Fatalf("size=%d error=%v", size, err)
					}
					if parsed == oversized || requests.Load() != 1 {
						t.Fatalf("parsed=%v requests=%d", parsed, requests.Load())
					}
					got := control.Snapshot()
					if got.Admission.Reserved != 0 || got.Metrics.Counters["body_bytes"] != int64(size) || got.Metrics.Counters["decode_errors"] != 0 {
						t.Fatalf("unexpected observations: %+v", got)
					}
					if oversized && (got.Metrics.Counters["body_errors"] != 1 || got.Metrics.Counters["page_errors"] != 1) {
						t.Fatalf("missing errors: %+v", got)
					}
				})
			}
		}
	}
}

type oversizedPageBody struct {
	read   int
	closes int
}

func (b *oversizedPageBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	b.read += len(p)
	return len(p), nil // Model an endless response without allocating its contents.
}
func (b *oversizedPageBody) Close() error { b.closes++; return nil }

func TestFetchPageStopsReadingHugeBody(t *testing.T) {
	for _, measured := range []bool{false, true} {
		t.Run(strconv.FormatBool(measured), func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			body := &oversizedPageBody{}
			http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: body, ContentLength: 1 << 40, Header: make(http.Header), Request: r}, nil
			})
			var control *HTTPControl
			if measured {
				control = NewHTTPControl(1, true)
			}
			_, err := fetchPage(context.Background(), "http://fixture.invalid", time.Second, 3, nil, slog.New(slog.DiscardHandler), func([]byte) (parsedPage, error) { t.Fatal("oversized body decoded"); return parsedPage{}, nil }, control)
			if !errors.Is(err, errPageBodyTooLarge) || err.Error() != "response body exceeds 8 MiB limit" || body.read != maxPageBodyBytes+1 || body.closes != 1 {
				t.Fatalf("err=%v read=%d closes=%d", err, body.read, body.closes)
			}
		})
	}
}

func TestFetchPageFullFixturePage(t *testing.T) {
	raw, err := os.ReadFile("testdata/nvapi_user_videos_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload NicoData
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	// Expand the checked-in two-item contract fixture to the requested pageSize=100.
	items := payload.Data.Items
	for len(payload.Data.Items) < 100 {
		payload.Data.Items = append(payload.Data.Items, items...)
	}
	count := 100
	payload.Data.TotalCount = &count
	raw, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("100-item synthetic fixture page: %d bytes; limit: %d bytes", len(raw), maxPageBodyBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) }))
	defer server.Close()
	page, err := fetchPage(context.Background(), server.URL, time.Second, 1, nil, slog.New(slog.DiscardHandler), parseUserVideoPage, nil)
	if err != nil || len(page.Items) != 100 {
		t.Fatalf("items=%d error=%v", len(page.Items), err)
	}
}
