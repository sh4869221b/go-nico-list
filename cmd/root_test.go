package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func TestRetriesValidation(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	retries = 0
	t.Cleanup(func() { retries = defaultRetries })

	rootCmd.SetArgs([]string{"12345"})
	err := rootCmd.Execute()
	if err == nil || err.Error() != "retries must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInvalidInput(t *testing.T) {
	// prepare custom progress bar to capture completion state
	var bar *progressbar.ProgressBar
	origProgressBarNew := progressBarNew
	origIsTerminal := isTerminal
	progressBarNew = func(max int64, writer io.Writer, visible bool) *progressbar.ProgressBar {
		bar = progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(writer),
			progressbar.OptionSetVisibility(visible),
		)
		return bar
	}
	isTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() {
		progressBarNew = origProgressBarNew
		isTerminal = origIsTerminal
	})

	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	if err := runRootCmd(nil, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bar == nil || !bar.IsFinished() {
		t.Errorf("progress bar not finished")
	}
}

func TestProgressAutoDisabledOnNonTTY(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	origProgressBarNew := progressBarNew
	origIsTerminal := isTerminal
	origForceProgress := forceProgress
	origNoProgress := noProgress
	t.Cleanup(func() {
		progressBarNew = origProgressBarNew
		isTerminal = origIsTerminal
		forceProgress = origForceProgress
		noProgress = origNoProgress
	})

	var visible bool
	progressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(io.Discard),
			progressbar.OptionSetVisibility(show),
		)
	}
	isTerminal = func(io.Writer) bool { return false }
	forceProgress = false
	noProgress = false

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if visible {
		t.Errorf("expected progress to be hidden on non-TTY stderr")
	}
}

func TestProgressForcedOn(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	origProgressBarNew := progressBarNew
	origIsTerminal := isTerminal
	origForceProgress := forceProgress
	origNoProgress := noProgress
	t.Cleanup(func() {
		progressBarNew = origProgressBarNew
		isTerminal = origIsTerminal
		forceProgress = origForceProgress
		noProgress = origNoProgress
	})

	var visible bool
	progressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(io.Discard),
			progressbar.OptionSetVisibility(show),
		)
	}
	isTerminal = func(io.Writer) bool { return false }
	forceProgress = true
	noProgress = false

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !visible {
		t.Errorf("expected progress to be visible when forced")
	}
}

func TestNoProgressOverridesForceProgress(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	origProgressBarNew := progressBarNew
	origIsTerminal := isTerminal
	origForceProgress := forceProgress
	origNoProgress := noProgress
	t.Cleanup(func() {
		progressBarNew = origProgressBarNew
		isTerminal = origIsTerminal
		forceProgress = origForceProgress
		noProgress = origNoProgress
	})

	var visible bool
	progressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(io.Discard),
			progressbar.OptionSetVisibility(show),
		)
	}
	isTerminal = func(io.Writer) bool { return true }
	forceProgress = true
	noProgress = true

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if visible {
		t.Errorf("expected progress to be hidden when no-progress is set")
	}
}

func TestRunRootCmdEmitsSummary(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	origNoProgress := noProgress
	origForceProgress := forceProgress
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := errOut.String()
	want := "summary inputs=2 valid=1 invalid=1 fetch_ok=1 fetch_err=0 output_count=0"
	if !strings.Contains(got, want) {
		t.Fatalf("expected summary %q, got %q", want, got)
	}
}

func TestRunRootCmdStrictInvalidInputReturnsError(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	origStrict := strictInput
	origNoProgress := noProgress
	origForceProgress := forceProgress
	strictInput = true
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		strictInput = origStrict
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdStrictInvalidStillOutputsValidResults(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	origStrict := strictInput
	origNoProgress := noProgress
	origForceProgress := forceProgress
	strictInput = true
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		strictInput = origStrict
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "invalid"}); err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdBestEffortReturnsNilOnFetchError(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	origBestEffort := bestEffort
	origNoProgress := noProgress
	origForceProgress := forceProgress
	bestEffort = true
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		bestEffort = origBestEffort
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdDedupeRemovesDuplicates(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	origDedupe := dedupeOutput
	origNoProgress := noProgress
	origForceProgress := forceProgress
	dedupeOutput = true
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		dedupeOutput = origDedupe
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdStrictOverridesBestEffort(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	origBestEffort := bestEffort
	origStrict := strictInput
	origNoProgress := noProgress
	origForceProgress := forceProgress
	bestEffort = true
	strictInput = true
	noProgress = true
	forceProgress = false
	t.Cleanup(func() {
		bestEffort = origBestEffort
		strictInput = origStrict
		noProgress = origNoProgress
		forceProgress = origForceProgress
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInvalidInputNoOutput(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdNoInputs(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"
	inputFilePath = ""
	readStdin = false
	t.Cleanup(func() {
		inputFilePath = ""
		readStdin = false
	})

	if err := runRootCmd(nil, nil); err == nil || err.Error() != "no inputs provided" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInputFileNoArgs(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "inputs.txt")
	if err := os.WriteFile(inputPath, []byte("nicovideo.jp/user/1\ninvalid\n\n"), 0o644); err != nil {
		t.Fatalf("failed to write inputs: %v", err)
	}
	inputFilePath = inputPath
	readStdin = false
	t.Cleanup(func() {
		inputFilePath = ""
		readStdin = false
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdStdinNoArgs(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	inputFilePath = ""
	readStdin = true
	t.Cleanup(func() {
		inputFilePath = ""
		readStdin = false
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader("nicovideo.jp/user/1\n"))
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

type blockingErrorReader struct {
	wait <-chan struct{}
	err  error
}

func (r blockingErrorReader) Read(p []byte) (int, error) {
	<-r.wait
	return 0, r.err
}

func TestRunRootCmdInputReadErrorCancelsFetches(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	started := make(chan struct{})
	canceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			canceledOnce.Do(func() { close(canceled) })
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
		}
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = 5 * time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	inputFilePath = ""
	readStdin = true
	t.Cleanup(func() {
		inputFilePath = ""
		readStdin = false
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	wait := make(chan struct{})
	cmd.SetIn(blockingErrorReader{wait: wait, err: errors.New("stdin read error")})

	errCh := make(chan error, 1)
	go func() {
		errCh <- runRootCmd(cmd, []string{"nicovideo.jp/user/1"})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected request to start")
	}

	close(wait)

	select {
	case err := <-errCh:
		if err == nil || err.Error() != "stdin read error" {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected command to finish after input error")
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected request to be canceled")
	}
}

func TestRunRootCmdInputFileReadErrorCancelsFetches(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	started := make(chan struct{})
	canceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			canceledOnce.Do(func() { close(canceled) })
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
		}
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = 5 * time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	longLine := strings.Repeat("a", 1024*1024+1)
	pr, pw := io.Pipe()
	oldOpenInputFile := openInputFile
	openInputFile = func(string) (io.ReadCloser, error) {
		return pr, nil
	}
	t.Cleanup(func() { openInputFile = oldOpenInputFile })
	inputFilePath = "dummy"
	readStdin = false
	t.Cleanup(func() {
		inputFilePath = ""
		readStdin = false
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- runRootCmd(cmd, []string{"nicovideo.jp/user/1"})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected request to start")
	}

	go func() {
		_, _ = pw.Write([]byte(longLine + "\n"))
		_ = pw.Close()
	}()

	select {
	case err := <-errCh:
		if err == nil || !errors.Is(err, bufio.ErrTooLong) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected command to finish after input error")
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected request to be canceled")
	}
}

func TestRunRootCmdPartialFailureOutputsResults(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

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

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2"}); err == nil {
		t.Fatalf("expected error")
	}
	if got := out.String(); got != "sm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdLogFile(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "app.log")
	oldLogFilePath := logFilePath
	logFilePath = logPath
	t.Cleanup(func() { logFilePath = oldLogFilePath })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"12345"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected logfile to contain logs")
	}
}

func TestRunRootCmdLogFileMultipleErrors(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "app.log")
	oldLogFilePath := logFilePath
	logFilePath = logPath
	t.Cleanup(func() { logFilePath = oldLogFilePath })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2"}); err == nil {
		t.Fatalf("expected error")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if got := bytes.Count(data, []byte("failed to get video list")); got < 2 {
		t.Fatalf("expected at least 2 error logs, got %d", got)
	}
}

func TestConcurrencyValidation(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	concurrency = 0
	t.Cleanup(func() { concurrency = 3 })

	rootCmd.SetArgs([]string{"12345"})
	err := rootCmd.Execute()
	if err == nil || err.Error() != "concurrency must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}
