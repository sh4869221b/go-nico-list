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

// writeLineOutput writes line output directly without building a joined string.
func writeLineOutput(out io.Writer, items []string, withURL bool) error {
	return writeLineBatch(bufio.NewWriter(out), items, withURL)
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
