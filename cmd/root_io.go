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

// newProgressBar creates a progress bar configured for the current run.
func newProgressBar(cmd *cobra.Command, totalKnown bool, total int64) *progressbar.ProgressBar {
	if !totalKnown {
		total = -1
	}
	var errWriter io.Writer = os.Stderr
	if cmd != nil {
		errWriter = cmd.ErrOrStderr()
	}
	visible := shouldShowProgress(errWriter)
	writer := errWriter
	if !visible {
		writer = io.Discard
	}
	return progressBarNew(total, writer, visible)
}

// shouldShowProgress reports whether progress output should be visible.
func shouldShowProgress(errWriter io.Writer) bool {
	visible := isTerminal(errWriter)
	if forceProgress {
		visible = true
	}
	if noProgress {
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

// streamInputs streams inputs from args, input files, and stdin.
func streamInputs(cmd *cobra.Command, args []string) inputStream {
	out := make(chan string)
	errCh := make(chan error, 1)
	totalKnown := inputFilePath == "" && !readStdin
	total := int64(len(args))

	go func() {
		defer close(out)
		defer close(errCh)

		count := 0
		for _, arg := range args {
			out <- arg
			count++
		}

		if inputFilePath != "" {
			n, err := streamLinesFromFile(inputFilePath, out)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if readStdin {
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
func streamLinesFromFile(path string, out chan<- string) (int, error) {
	file, err := openInputFile(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return streamLines(file, out)
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
