//go:build linux || darwin

package logfile

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// Open opens a log destination with cancelable FIFO waiting.
func Open(ctx context.Context, path string, wait FIFOOpener) (io.WriteCloser, error) {
	flags := openFlags | unix.O_NONBLOCK
	var ticker *time.Ticker
	for ctx.Err() == nil {
		file, err := os.OpenFile(path, flags, 0o644)
		if !errors.Is(err, unix.ENXIO) {
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			// Darwin's os.File poller does not support FIFOs. Preserve
			// blocking writes after the nonblocking open succeeds.
			if runtime.GOOS == "darwin" {
				if err := unix.SetNonblock(int(file.Fd()), false); err != nil {
					_ = file.Close()
					return nil, &os.PathError{Op: "set blocking", Path: path, Err: err}
				}
			}
			return file, nil
		}
		if ticker == nil {
			info, statErr := os.Stat(path)
			if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
				return nil, err
			}
			if wait != nil {
				file, err := wait(ctx, path, info)
				if !errors.Is(err, ErrHelperUnavailable) {
					return file, err
				}
			}
			ticker = time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			flags &^= os.O_CREATE
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil, ctx.Err()
}
