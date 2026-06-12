package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkBuildJSONOutputLarge(b *testing.B) {
	targets := make([]targetResult, 100)
	outputIDs := make([]string, 0, 5000)
	for i := range targets {
		items := make([]string, 50)
		for j := range items {
			items[j] = fmt.Sprintf("sm%d", i*50+j)
		}
		targets[i] = targetResult{Type: targetTypeUser, ID: fmt.Sprintf("%d", i), Items: items}
		outputIDs = append(outputIDs, items...)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		payload := buildJSONOutput(100, 100, 0, nil, targets, nil, len(outputIDs), outputIDs)
		if payload.OutputCount != len(outputIDs) {
			b.Fatalf("unexpected output_count: %d", payload.OutputCount)
		}
	}
}

func BenchmarkSortTargetResultsLarge(b *testing.B) {
	base := make([]targetResult, 2000)
	for i := range base {
		targetType := targetTypeUser
		if i%2 == 0 {
			targetType = targetTypeMylist
		}
		id := fmt.Sprintf("%d", 2000-i)
		if i%5 == 0 {
			id = fmt.Sprintf("%d-invalid", i)
		}
		base[i] = targetResult{Type: targetType, ID: id, Error: fmt.Sprintf("err-%d", i)}
	}
	results := make([]targetResult, len(base))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(results, base)
		sortTargetResults(results)
	}
}

func BenchmarkRunRootCmdLargeFanInLineOutput(b *testing.B) {
	server := newBenchmarkAPIServer(b)
	cfg := testFetchConfig(server.URL)
	cfg.NoProgress = true
	args := []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/mylist/847130"}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		deps := newTestRootDeps()
		deps.Stdout = io.Discard
		deps.Stderr = io.Discard
		err := executeBenchmarkRootCommand(cfg, deps, args...)
		if err != nil {
			b.Fatalf("command returned error: %v", err)
		}
	}
}

func BenchmarkRunRootCmdLargeFanInJSONOutput(b *testing.B) {
	server := newBenchmarkAPIServer(b)
	cfg := testFetchConfig(server.URL)
	cfg.NoProgress = true
	cfg.JSONOutput = true
	args := []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/mylist/847130"}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		deps := newTestRootDeps()
		deps.Stdout = io.Discard
		deps.Stderr = io.Discard
		err := executeBenchmarkRootCommand(cfg, deps, args...)
		if err != nil {
			b.Fatalf("command returned error: %v", err)
		}
	}
}

func executeBenchmarkRootCommand(cfg RootConfig, deps RootDeps, args ...string) error {
	cmd := NewRootCommand(cfg, deps)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newBenchmarkAPIServer(b *testing.B) *httptest.Server {
	b.Helper()
	userPayload := benchmarkUserPayload(100)
	mylistPayload := benchmarkMylistPayload(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			if r.URL.Path == "/mylists/847130" {
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[]}}}`)
				return
			}
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		if r.URL.Path == "/mylists/847130" {
			_, _ = io.WriteString(w, mylistPayload)
			return
		}
		_, _ = io.WriteString(w, userPayload)
	}))
	b.Cleanup(server.Close)
	return server
}

func benchmarkUserPayload(count int) string {
	payload := `{"meta":{"status":200},"data":{"items":[`
	for i := range count {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf(`{"essential":{"id":"sm%d","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12}}}`, i)
	}
	return payload + `]}}`
}

func benchmarkMylistPayload(count int) string {
	payload := `{"meta":{"status":200},"data":{"mylist":{"items":[`
	for i := range count {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf(`{"video":{"id":"sm%d","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12}}}`, i)
	}
	return payload + `]}}}`
}
