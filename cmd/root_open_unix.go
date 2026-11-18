//go:build linux || darwin

package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const fifoWait = 25 * time.Millisecond

func openOSInputFile(ctx context.Context, path string, detach func()) (io.ReadCloser, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		return file, nil
	}
	if !canWaitForFIFO(file.Fd()) {
		_ = file.Close()
		detach()
		return os.Open(path)
	}
	conn, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &fifoInput{File: file, conn: conn, ctx: ctx}, nil
}

type fifoInput struct {
	*os.File
	conn syscall.RawConn
	ctx  context.Context
}

// Read waits for data or for a writer to disconnect. Reading a nonblocking
// FIFO directly before its first writer connects can return a premature EOF.
func (f *fifoInput) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for f.ctx.Err() == nil {
		var n int
		var ready bool
		var readErr error
		err := f.conn.Control(func(fd uintptr) {
			ready, readErr = waitForFIFO(fd)
			if ready && readErr == nil {
				n, readErr = unix.Read(int(fd), p)
			}
		})
		if err != nil {
			return 0, err
		}
		if errors.Is(readErr, unix.EINTR) || errors.Is(readErr, unix.EAGAIN) {
			continue
		}
		if readErr != nil {
			return 0, &os.PathError{Op: "read", Path: f.Name(), Err: readErr}
		}
		if ready {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}
	}
	return 0, f.ctx.Err()
}
