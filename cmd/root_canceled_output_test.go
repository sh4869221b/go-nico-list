package cmd

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

func TestCancellationDoesNotWaitForCallerStdin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		defer close(release)
		command, _, _ := newTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
		command.SetArgs([]string{"--stdin", "--json"})
		command.SetIn(blockingErrorReader{wait: release, err: io.EOF})
		done := make(chan error, 1)
		go func() { done <- command.ExecuteContext(ctx) }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("command waited for the caller's stdin reader")
		}
	})
}

func TestNoSortDrainsCompletedBatchesAfterInputError(t *testing.T) {
	inputErr := errors.New("input read failed")
	outputErr := errors.New("output write failed")
	for _, failWrite := range []bool{false, true} {
		name := "drain"
		if failWrite {
			name = "writer error"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				originalTransport := http.DefaultTransport
				defer func() { http.DefaultTransport = originalTransport }()
				http.DefaultTransport = adaptiveInputErrorTransport(func(r *http.Request) (*http.Response, error) {
					id := strings.Split(r.URL.Path, "/")[2]
					body := httpCommandPagePayload(false, 1, []string{"sm" + id})
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})

				cfg := testFetchConfig("http://fixture.invalid")
				cfg.NoSortOutput, cfg.ReadStdin, cfg.Concurrency, cfg.PageConcurrency = true, true, 3, 2
				deps := newTestRootDeps()
				inputReady, outputReady := make(chan struct{}), make(chan struct{})
				var stdout bytes.Buffer
				deps.Stdout = writerFunc(func(p []byte) (int, error) {
					<-outputReady
					if failWrite {
						return 0, outputErr
					}
					return stdout.Write(p)
				})
				command, _, stderr := newTestRootCommand(t, cfg, deps)
				command.SetIn(blockingErrorReader{wait: inputReady, err: inputErr})
				command.SetArgs([]string{"nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/user/3"})
				done := make(chan error, 1)
				go func() { done <- command.ExecuteContext(t.Context()) }()
				// One batch is blocked in stdout, another is queued, and
				// the last worker is waiting for the writer to accept its batch.
				synctest.Wait()
				close(inputReady)
				synctest.Wait()
				close(outputReady)
				wantErr := inputErr
				if failWrite {
					wantErr = outputErr
				}
				if err := <-done; !errors.Is(err, wantErr) {
					t.Fatalf("error = %v, want %v", err, wantErr)
				}
				if failWrite {
					if !strings.Contains(stderr.String(), "output_count=0\n") {
						t.Fatalf("summary = %q", stderr.String())
					}
					return
				}
				for _, id := range []string{"sm1\n", "sm2\n", "sm3\n"} {
					if !strings.Contains(stdout.String(), id) {
						t.Errorf("missing %q in stdout %q", id, stdout.String())
					}
				}
				if !strings.Contains(stderr.String(), "output_count=3\n") {
					t.Fatalf("summary = %q", stderr.String())
				}
			})
		})
	}
}

func TestJSONRetainsTargetThatWasWaitingAtCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		originalTransport := http.DefaultTransport
		defer func() { http.DefaultTransport = originalTransport }()
		requests := make(chan string, 2)
		http.DefaultTransport = adaptiveInputErrorTransport(func(r *http.Request) (*http.Response, error) {
			requests <- r.URL.Path
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		command, stdout, stderr := newTestRootCommand(t, testFetchConfig("http://fixture.invalid"), newTestRootDeps())
		command.SetArgs([]string{"--json", "-T1", "nicovideo.jp/user/1", "nicovideo.jp/user/2"})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- command.ExecuteContext(ctx) }()
		synctest.Wait()
		if len(requests) != 1 || <-requests != "/users/1/videos" {
			t.Fatal("the first target should be the only one dispatched before cancellation")
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(requests) != 0 {
			t.Fatal("the queued target dispatched HTTP after cancellation")
		}
		var payload jsonOutputPayload
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Inputs != (jsonInputs{Total: 2, Valid: 2}) || len(payload.Targets) != 2 {
			t.Fatalf("input or queued target missing: %+v", payload)
		}
		queued := payload.Targets[1]
		if queued.ID != "2" || len(queued.Items) != 0 || queued.Error != "" {
			t.Fatalf("queued target = %+v", queued)
		}
		if !strings.Contains(stderr.String(), "summary inputs=2 valid=2 invalid=0 fetch_ok=2 fetch_err=0 output_count=0\n") {
			t.Fatalf("summary = %q", stderr.String())
		}
	})
}
