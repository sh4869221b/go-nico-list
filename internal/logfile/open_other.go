//go:build !linux && !darwin

package logfile

import (
	"context"
	"io"
)

// Open opens a log destination on platforms without the Unix FIFO opener.
func Open(ctx context.Context, path string, _ FIFOOpener) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return OpenFile(path)
}
