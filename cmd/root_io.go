package cmd

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// inputStream bundles input channels and total count metadata.
type inputStream struct {
	inputs     <-chan string
	errs       <-chan error
	totalKnown bool
	total      int64
}

func newProgressBarWithConfig(cmd *cobra.Command, totalKnown bool, total int64, cfg *RootConfig, deps RootDeps) *progressbar.ProgressBar {
	deps = normalizeRootDeps(deps)
	if !totalKnown {
		total = -1
	}
	var errWriter io.Writer = os.Stderr
	if cmd != nil {
		errWriter = cmd.ErrOrStderr()
	}
	visible := shouldShowProgressWithConfig(errWriter, cfg, deps)
	writer := errWriter
	if !visible {
		writer = io.Discard
	}
	return deps.ProgressBarNew(total, writer, visible)
}

func shouldShowProgressWithConfig(errWriter io.Writer, cfg *RootConfig, deps RootDeps) bool {
	deps = normalizeRootDeps(deps)
	visible := deps.IsTerminal(errWriter)
	if cfg.ForceProgress {
		visible = true
	}
	if cfg.NoProgress {
		visible = false
	}
	return visible
}

// defaultIsTerminal reports whether the writer is a terminal.
func defaultIsTerminal(w io.Writer) bool {
	if file, ok := w.(*os.File); ok {
		return term.IsTerminal(int(file.Fd()))
	}
	return false
}

func streamInputsWithConfig(cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) inputStream {
	deps = normalizeRootDeps(deps)
	out := make(chan string)
	errCh := make(chan error, 1)
	totalKnown := cfg.InputFilePath == "" && !cfg.ReadStdin
	total := int64(len(args))

	go func() {
		defer close(out)
		defer close(errCh)

		count := 0
		for _, arg := range args {
			out <- arg
			count++
		}

		if cfg.InputFilePath != "" {
			n, err := streamLinesFromFile(cfg.InputFilePath, out, deps)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if cfg.ReadStdin {
			var reader io.Reader = os.Stdin
			if cmd != nil {
				reader = cmd.InOrStdin()
			}
			n, err := streamLines(reader, out)
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
		inputs:     out,
		errs:       errCh,
		totalKnown: totalKnown,
		total:      total,
	}
}

// streamLinesFromFile streams trimmed lines from a file into out.
func streamLinesFromFile(path string, out chan<- string, deps RootDeps) (int, error) {
	deps = normalizeRootDeps(deps)
	file, err := deps.OpenInputFile(path)
	if err != nil {
		return 0, err
	}
	count, err := streamLines(file, out)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	return count, err
}

// streamLines streams non-empty trimmed lines from a reader into out.
func streamLines(reader io.Reader, out chan<- string) (int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out <- line
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	return count, nil
}
