package logfile

import (
	"context"
	"errors"
	"io"
	"os"
)

const openFlags = os.O_APPEND | os.O_CREATE | os.O_WRONLY

// FIFOOpener waits for a reader on an existing named pipe.
type FIFOOpener func(context.Context, string, os.FileInfo) (io.WriteCloser, error)

// ErrHelperUnavailable tells Open to use the in-process FIFO retry loop.
var ErrHelperUnavailable = errors.New("log FIFO helper unavailable")

// OpenFile is the default, context-free log opener exposed to callers.
func OpenFile(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, openFlags, 0o644)
}
