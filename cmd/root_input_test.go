package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunRootCmdNoInputs(t *testing.T) {
	_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
	if err == nil || err.Error() != "no inputs provided" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInputSourcesWithoutArgs(t *testing.T) {
	server := newEmptyAPIServer(t)
	const inputs = "nicovideo.jp/user/1\ninvalid\n\n"
	for _, stdin := range []bool{false, true} {
		t.Run(fmt.Sprintf("stdin=%t", stdin), func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.ReadStdin = stdin
			if !stdin {
				cfg.InputFilePath = filepath.Join(t.TempDir(), "inputs.txt")
				if err := os.WriteFile(cfg.InputFilePath, []byte(inputs), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd, out, errOut := newTestRootCommand(t, cfg, newTestRootDeps())
			cmd.SetIn(strings.NewReader(inputs))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "summary inputs=2 valid=1 invalid=1 fetch_ok=1 fetch_err=0 output_count=0"
			if out.Len() != 0 || !strings.Contains(errOut.String(), want) {
				t.Fatalf("stdout=%q stderr=%q, want empty output and %q", out.String(), errOut.String(), want)
			}
		})
	}
}

func TestRunRootCmdInputFileOpenError(t *testing.T) {
	openErr := errors.New("open failed")
	cfg := newTestRootConfig()
	cfg.InputFilePath = "dummy"
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) {
		return nil, openErr
	}

	_, _, err := executeTestRootCommand(t, cfg, deps)
	if !errors.Is(err, openErr) {
		t.Fatalf("expected open error, got %v", err)
	}
}

func TestRunRootCmdInputFileCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	cfg.InputFilePath = "dummy"
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) {
		return closeErrorReader{Reader: strings.NewReader("nicovideo.jp/user/1\n"), err: closeErr}, nil
	}

	_, _, err := executeTestRootCommand(t, cfg, deps)
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
}

func TestRunRootCmdInputReadErrorCancelsFetches(t *testing.T) {
	for _, test := range []struct {
		name       string
		file, json bool
	}{
		{name: "stdin"},
		{name: "json_stdin", json: true},
		{name: "oversized_file", file: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			var startedOnce, canceledOnce sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				startedOnce.Do(func() { close(started) })
				<-r.Context().Done()
				canceledOnce.Do(func() { close(canceled) })
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			cfg := testFetchConfig(server.URL)
			cfg.HTTPClientTimeout, cfg.JSONOutput = 5*time.Second, test.json
			cfg.ReadStdin = !test.file
			deps := newTestRootDeps()
			if test.file {
				cfg.InputFilePath = "dummy"
				deps.OpenInputFile = func(string) (io.ReadCloser, error) { return reader, nil }
			}
			cmd, _, _ := newTestRootCommand(t, cfg, deps)
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"nicovideo.jp/user/1"})
			cmd.SetIn(reader)
			done := make(chan error, 1)
			go func() { done <- cmd.Execute() }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("request did not start")
			}
			wantErr := errors.New("stdin read error")
			if test.file {
				wantErr = bufio.ErrTooLong
				go func() {
					_, _ = io.WriteString(writer, strings.Repeat("a", 1024*1024+1)+"\n")
					_ = writer.Close()
				}()
			} else {
				_ = writer.CloseWithError(wantErr)
			}
			select {
			case err := <-done:
				if !errors.Is(err, wantErr) {
					t.Fatalf("expected input error %v, got %v", wantErr, err)
				}
			case <-ctx.Done():
				t.Fatal("command did not finish after input error")
			}
			select {
			case <-canceled:
			case <-ctx.Done():
				t.Fatal("request was not canceled")
			}
		})
	}
}
