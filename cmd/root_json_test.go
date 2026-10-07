package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRunRootCmdJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.JSONOutput = true
	cfg.URL = true
	cfg.DedupeOutput = true

	out, errOut, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload jsonOutputPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inputs", "invalid", "targets", "errors", "output_count", "items"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("stdout JSON missing %q", name)
		}
	}
	if len(fields) != 6 {
		t.Fatalf("unexpected JSON fields: %v", fields)
	}

	if payload.Inputs.Total != 2 || payload.Inputs.Valid != 1 || payload.Inputs.Invalid != 1 {
		t.Errorf("unexpected inputs: %+v", payload.Inputs)
	}
	if len(payload.Invalid) != 1 || payload.Invalid[0] != "invalid" {
		t.Errorf("unexpected invalid list: %+v", payload.Invalid)
	}
	if len(payload.Targets) != 1 {
		t.Fatalf("unexpected targets length: %d", len(payload.Targets))
	}
	target := payload.Targets[0]
	if target.Type != targetTypeUser || target.ID != "1" || target.Error != "" {
		t.Errorf("unexpected target: %+v", target)
	}
	if got := strings.Join(target.Items, ","); got != "sm2,sm1,sm1" {
		t.Errorf("unexpected target items: %v", target.Items)
	}
	if payload.OutputCount != 2 {
		t.Errorf("unexpected output_count: %d", payload.OutputCount)
	}
	if got := strings.Join(payload.Items, ","); got != "sm1,sm2" {
		t.Errorf("unexpected items: %v", payload.Items)
	}
	if len(payload.Errors) != 0 {
		t.Errorf("unexpected errors: %v", payload.Errors)
	}
	if !strings.Contains(errOut.String(), "summary inputs=2 valid=1 invalid=1") {
		t.Errorf("expected summary in stderr, got %q", errOut.String())
	}
}

func TestRunRootCmdJSONNoSortPreservesOutputItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.JSONOutput = true
	cfg.NoSortOutput = true

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload jsonOutputPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if got := strings.Join(payload.Items, ","); got != "sm2,sm1" {
		t.Errorf("unexpected items: %v", payload.Items)
	}
}

func TestRunRootCmdJSONOutputWithFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/users/1/") {
			if r.URL.Query().Get("page") != "1" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.JSONOutput = true

	out, errOut, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "nicovideo.jp/user/2")
	if err == nil {
		t.Fatalf("expected error")
	}

	var payload jsonOutputPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if payload.Inputs.Total != 2 || payload.Inputs.Valid != 2 || payload.Inputs.Invalid != 0 {
		t.Errorf("unexpected inputs: %+v", payload.Inputs)
	}
	if len(payload.Targets) != 2 {
		t.Fatalf("unexpected targets length: %d", len(payload.Targets))
	}
	if payload.Targets[0].Type != targetTypeUser || payload.Targets[0].ID != "1" || strings.Join(payload.Targets[0].Items, ",") != "sm1" {
		t.Errorf("unexpected target1: %+v", payload.Targets[0])
	}
	if payload.Targets[1].Type != targetTypeUser || payload.Targets[1].ID != "2" || payload.Targets[1].Error == "" || len(payload.Targets[1].Items) != 0 {
		t.Errorf("unexpected target2: %+v", payload.Targets[1])
	}
	if payload.OutputCount != 1 || strings.Join(payload.Items, ",") != "sm1" || len(payload.Errors) != 1 {
		t.Errorf("unexpected payload: %+v", payload)
	}
	if !strings.Contains(errOut.String(), "fetch_err=1") {
		t.Errorf("expected fetch_err=1 in summary, got %q", errOut.String())
	}
}

func TestRunRootCmdJSONOutputTargetsSortedByTypeAndID(t *testing.T) {
	user2Completed := make(chan struct{})
	var user2CompletedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/mylists/") {
			ids := []string{"sm42"}
			if r.URL.Query().Get("page") != "1" {
				ids = nil
			}
			_, _ = io.WriteString(w, httpCommandPagePayload(true, 1, ids))
			return
		}
		isUser2 := strings.Contains(r.URL.Path, "/users/2/")
		if r.URL.Query().Get("page") != "1" {
			if isUser2 {
				user2CompletedOnce.Do(func() { close(user2Completed) })
			}
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		if strings.Contains(r.URL.Path, "/users/1/") {
			select {
			case <-user2Completed:
			case <-r.Context().Done():
				http.Error(w, "request canceled before user 2 completed", http.StatusGatewayTimeout)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.Concurrency = 3
	cfg.JSONOutput = true

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/mylist/847130")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload jsonOutputPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if len(payload.Targets) != 3 {
		t.Fatalf("unexpected targets length: %d", len(payload.Targets))
	}
	if payload.Targets[1].Type != targetTypeUser || payload.Targets[1].ID != "1" || payload.Targets[2].Type != targetTypeUser || payload.Targets[2].ID != "2" {
		t.Fatalf("expected targets ordered by type and id, got %+v", payload.Targets)
	}
	mylist := payload.Targets[0]
	if mylist.Type != targetTypeMylist || mylist.ID != "847130" || strings.Join(mylist.Items, ",") != "sm42" || mylist.Error != "" {
		t.Fatalf("unexpected mylist target: %+v", mylist)
	}
}

func TestSortTargetResultsSortsByTypeAndNumericID(t *testing.T) {
	results := []targetResult{{Type: targetTypeUser, ID: "10"}, {Type: targetTypeMylist, ID: "2"}, {Type: targetTypeUser, ID: "1"}, {Type: targetTypeMylist, ID: "11"}, {Type: targetTypeMylist, ID: "10000000000"}, {Type: targetTypeMylist, ID: "9999999999"}, {Type: targetTypeUser, ID: "000000000000"}, {Type: targetTypeMylist, ID: "02"}, {Type: targetTypeMylist, ID: "2", Error: "b"}, {Type: targetTypeMylist, ID: "2", Error: "a"}}
	sortTargetResults(results)
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.Type+":"+result.ID+":"+result.Error)
	}
	if strings.Join(got, ",") != "mylist:02:,mylist:2:,mylist:2:a,mylist:2:b,mylist:11:,mylist:9999999999:,mylist:10000000000:,user:000000000000:,user:1:,user:10:" {
		t.Fatalf("unexpected target order: %v", got)
	}
}
