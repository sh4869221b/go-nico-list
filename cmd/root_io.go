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
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var errInputLogConflict = errors.New("input-file and logfile must refer to different files")

// validateInputLogPaths catches aliases before the log is opened, which can
// itself block when both paths refer to a named pipe.
func validateInputLogPaths(inputPath, logPath string) error {
	if inputPath == "" || logPath == "" {
		return nil
	}
	if inputPath == logPath {
		return errInputLogConflict
	}
	if inputInfo, err := os.Stat(inputPath); err == nil {
		if logInfo, err := os.Stat(logPath); err == nil && os.SameFile(inputInfo, logInfo) {
			return errInputLogConflict
		}
	}
	return nil
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

func streamInputsWithConfig(ctx context.Context, cancel context.CancelFunc, cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps, logInfo os.FileInfo) inputStream {
	out := make(chan string)
	errCh := make(chan error, 1)
	inputPath := cfg.InputFilePath
	var stdin io.Reader
	if cfg.ReadStdin {
		stdin = cmd.InOrStdin()
	}
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
			n, err := streamLinesFromFile(ctx, inputPath, out, deps, logInfo)
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
func streamLinesFromFile(ctx context.Context, path string, out chan<- string, deps RootDeps, logInfo os.FileInfo) (int, error) {
	openedFile, err := deps.OpenInputFile(path)
	if err != nil {
		return 0, err
	}
	file := &onceReadCloser{ReadCloser: openedFile}
	// Check the opened files too in case a path changed since validation.
	if logInfo != nil {
		if inputFile, ok := openedFile.(interface{ Stat() (os.FileInfo, error) }); ok {
			if info, err := inputFile.Stat(); err == nil && os.SameFile(info, logInfo) {
				_ = file.Close()
				return 0, errInputLogConflict
			}
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
