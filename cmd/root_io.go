package cmd

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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

func streamInputsWithConfig(ctx context.Context, cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) inputStream {
	out := make(chan string)
	errCh := make(chan error, 1)
	progressTotal := int64(len(args))
	if cfg.InputFilePath != "" || cfg.ReadStdin {
		progressTotal = -1
	}

	go func() {
		defer close(out)
		defer close(errCh)

		count := 0
		for _, arg := range args {
			if !sendInput(ctx, out, arg) {
				return
			}
			count++
		}

		if cfg.InputFilePath != "" {
			n, err := streamLinesFromFile(ctx, cfg.InputFilePath, out, deps)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if cfg.ReadStdin {
			n, err := streamLines(ctx, cmd.InOrStdin(), out)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if count == 0 {
			errCh <- errors.New("no inputs provided")
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
func streamLinesFromFile(ctx context.Context, path string, out chan<- string, deps RootDeps) (int, error) {
	openedFile, err := deps.OpenInputFile(path)
	if err != nil {
		return 0, err
	}
	file := &onceReadCloser{ReadCloser: openedFile}
	count, err := streamLines(ctx, file, out)
	if closeErr := file.Close(); err == nil && closeErr != nil && ctx.Err() == nil {
		err = closeErr
	}
	return count, err
}

// streamLines streams non-empty trimmed lines from a reader into out.
func streamLines(ctx context.Context, reader io.Reader, out chan<- string) (int, error) {
	done := make(chan struct{})
	var closedOnCancel atomic.Bool
	if closer, ok := reader.(io.Closer); ok {
		go func() {
			select {
			case <-ctx.Done():
				closedOnCancel.Store(true)
				_ = closer.Close()
			case <-done:
			}
		}()
		defer close(done)
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
	if err := scanner.Err(); err != nil {
		if closedOnCancel.Load() {
			return count, nil
		}
		return count, err
	}
	return count, nil
}
