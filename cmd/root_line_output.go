package cmd

import (
	"bufio"
	"io"
)

// outputWriteResult records the output count reported in the summary and any write error.
type outputWriteResult struct {
	count int
	err   error
}

type outputByteCounter struct {
	out     io.Writer
	written int64
}

func (w *outputByteCounter) Write(p []byte) (int, error) {
	n, err := w.out.Write(p)
	w.written += int64(n)
	return n, err
}

type lineOutputWriter struct {
	buffer  *bufio.Writer
	counter *outputByteCounter
}

func newLineOutputWriter(out io.Writer) lineOutputWriter {
	counter := &outputByteCounter{out: out}
	return lineOutputWriter{buffer: bufio.NewWriter(counter), counter: counter}
}

// writeBatch counts complete records even when stdout accepts a partial write.
func (w lineOutputWriter) writeBatch(items []string, withURL bool) outputWriteResult {
	w.counter.written = 0
	err := writeLineBatch(w.buffer, items, withURL)
	written := w.counter.written
	if out, ok := w.counter.out.(*bufio.Writer); ok {
		// A supplied buffer may still hold the tail of this batch after a
		// downstream write error. Only count bytes it actually passed on.
		if flushErr := out.Flush(); err == nil {
			err = flushErr
		}
		written -= int64(out.Buffered())
	}
	if err != nil {
		lineOverhead := int64(1) // newline
		if withURL {
			lineOverhead += int64(len(nicoWatchURLPrefix))
		}
		count := 0
		for _, item := range items {
			written -= int64(len(item)) + lineOverhead
			if written < 0 {
				break
			}
			count++
		}
		return outputWriteResult{count: count, err: err}
	}
	return outputWriteResult{count: len(items)}
}

// writeLineBatch flushes the completed batch while retaining the buffer for reuse.
func writeLineBatch(writer *bufio.Writer, items []string, withURL bool) error {
	for _, item := range items {
		if withURL {
			if _, err := writer.WriteString(nicoWatchURLPrefix); err != nil {
				return err
			}
		}
		if _, err := writer.WriteString(item); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
	}
	return writer.Flush()
}
