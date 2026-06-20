package cmd

import (
	"bufio"
	"io"
)

// writeLineOutput writes line output directly without building a joined string.
func writeLineOutput(out io.Writer, items []string, withURL bool) error {
	if len(items) == 0 {
		return nil
	}
	writer := bufio.NewWriter(out)
	for _, item := range items {
		if withURL {
			if _, err := io.WriteString(writer, nicoWatchURLPrefix); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, item); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	return writer.Flush()
}
