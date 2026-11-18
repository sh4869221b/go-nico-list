package cmd

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

var errInputLogConflict = errors.New("input and logfile must refer to different files")

func openFileInfo(value any) os.FileInfo {
	if file, ok := value.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, _ := file.Stat()
		return info
	}
	return nil
}

func sameFile(left, right os.FileInfo) bool {
	return left != nil && right != nil && os.SameFile(left, right)
}

// validateInputLogPaths catches aliases before the log is opened, which can
// itself block when both paths refer to a named pipe.
func validateInputLogPaths(inputPath, logPath string, stdinInfo os.FileInfo) error {
	if inputPath == "" && stdinInfo == nil {
		return nil
	}
	if inputPath == logPath {
		return errInputLogConflict
	}
	if logInfo, err := os.Stat(logPath); err == nil {
		if sameFile(stdinInfo, logInfo) {
			return errInputLogConflict
		}
		if inputPath != "" {
			inputInfo, _ := os.Stat(inputPath)
			if sameFile(inputInfo, logInfo) {
				return errInputLogConflict
			}
		}
	}
	return nil
}

// guardedLog serializes conflict detection with all logging paths, including
// fetch workers and the final metrics log.
type guardedLog struct {
	writer  io.Writer
	info    os.FileInfo
	mu      sync.Mutex
	discard bool
}

func (l *guardedLog) checkInput(info os.FileInfo) error {
	if l == nil || !sameFile(l.info, info) {
		return nil
	}
	l.mu.Lock()
	l.discard = true
	l.mu.Unlock()
	return errInputLogConflict
}

func (l *guardedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.discard {
		return len(p), nil
	}
	return l.writer.Write(p)
}

// inputStream bundles input channels and total count metadata.
type inputStream struct {
	inputs        <-chan string
	errs          <-chan error
	progressTotal int64
}

// onceReadCloser ensures shared cancellation and cleanup paths close a reader once.
type onceReadCloser struct {
	io.ReadCloser
	closeOnce sync.Once
	closeErr  error
}

// Close closes the wrapped reader once and returns the first close error.
func (r *onceReadCloser) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.ReadCloser.Close()
	})
	return r.closeErr
}

// newInputProgress creates the progress bar with the writer selected by the visibility flags.
func newInputProgress(total int64, errWriter io.Writer, cfg *RootConfig, deps RootDeps) *progressbar.ProgressBar {
	visible := shouldShowProgressWithConfig(errWriter, cfg, deps)
	writer := errWriter
	if !visible {
		writer = io.Discard
	}
	return deps.ProgressBarNew(total, writer, visible)
}

func shouldShowProgressWithConfig(errWriter io.Writer, cfg *RootConfig, deps RootDeps) bool {
	visible := deps.IsTerminal(errWriter)
	return !cfg.NoProgress && (cfg.ForceProgress || visible)
}

// defaultIsTerminal reports whether the writer is a terminal.
func defaultIsTerminal(w io.Writer) bool {
	if file, ok := w.(*os.File); ok {
		return term.IsTerminal(int(file.Fd()))
	}
	return false
}

func streamInputsWithConfig(ctx context.Context, cancel context.CancelFunc, args []string, inputPath string, stdin io.Reader, deps RootDeps, log *guardedLog) inputStream {
	out := make(chan string)
	errCh := make(chan error, 1)
	progressTotal := int64(len(args))
	if inputPath != "" || stdin != nil {
		progressTotal = -1
	}

	go func() {
		defer close(out)
		defer close(errCh)
		reportError := func(err error) {
			errCh <- err
			cancel()
		}

		count := 0
		for _, arg := range args {
			if !sendInput(ctx, out, arg) {
				return
			}
			count++
		}

		if inputPath != "" && ctx.Err() == nil {
			n, err := streamLinesFromFile(ctx, inputPath, out, deps.OpenInputFile, log)
			count += n
			if err != nil {
				reportError(err)
				return
			}
		}

		if stdin != nil {
			n, err := streamLines(ctx, stdin, out)
			count += n
			if err != nil {
				reportError(err)
				return
			}
		}

		if count == 0 && ctx.Err() == nil {
			reportError(errors.New("no inputs provided"))
		}
	}()

	return inputStream{
		inputs:        out,
		errs:          errCh,
		progressTotal: progressTotal,
	}
}

// sendInput sends input unless ctx is canceled.
func sendInput(ctx context.Context, out chan<- string, input string) bool {
	select {
	case out <- input:
		return true
	case <-ctx.Done():
		return false
	}
}

// streamLinesFromFile streams trimmed lines from a file into out.
func streamLinesFromFile(ctx context.Context, path string, out chan<- string, openFile func(string) (io.ReadCloser, error), log *guardedLog) (int, error) {
	var openedFile io.ReadCloser
	var err error
	if openFile == nil {
		openedFile, err = openOSInputFile(ctx, path)
	} else {
		openedFile, err = openFile(path)
	}
	if err != nil {
		return 0, err
	}
	file := &onceReadCloser{ReadCloser: openedFile}
	// Check the opened files too in case a path changed since validation.
	if log != nil {
		if err := log.checkInput(openFileInfo(openedFile)); err != nil {
			_ = file.Close()
			return 0, err
		}
	}
	count, err := streamLines(ctx, file, out)
	if closeErr := file.Close(); err == nil && closeErr != nil && ctx.Err() == nil {
		err = closeErr
	}
	return count, err
}

// streamLines streams non-empty trimmed lines from a reader into out.
func streamLines(ctx context.Context, reader io.Reader, out chan<- string) (int, error) {
	select {
	case <-ctx.Done():
		return 0, nil
	default:
	}
	if closer, ok := reader.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !sendInput(ctx, out, line) {
			return count, nil
		}
		count++
	}
	if ctx.Err() == nil {
		return count, scanner.Err()
	}
	return count, nil
}
