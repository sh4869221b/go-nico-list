//go:build linux || darwin

package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
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

func openOSLogFile(ctx context.Context, path string) (io.WriteCloser, error) {
	flags := logOpenFlags | unix.O_NONBLOCK
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
			// blocking writes after the nonblocking open has succeeded.
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
			if anchor, pinned := pinLogFIFO(path, info); anchor != nil {
				return openPinnedLogFIFO(ctx, path, anchor, pinned)
			}
			ticker = time.NewTicker(fifoWait)
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

// A blocking open registers as a writer in the kernel throughout the wait,
// even if a reader opens and closes before the command can run again.
func openPinnedLogFIFO(ctx context.Context, path string, anchor *os.File, pinned string) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		_ = anchor.Close()
		return nil, err
	}
	type openResult struct {
		file *os.File
		err  error
	}
	opened := make(chan openResult)
	done := make(chan struct{})
	releaseAnchor := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = anchor.Close() }()
		defer func() { <-releaseAnchor }()
		if ctx.Err() != nil {
			return
		}
		file, err := os.OpenFile(pinned, os.O_WRONLY|os.O_APPEND, 0)
		select {
		case opened <- openResult{file, err}:
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case result := <-opened:
		close(releaseAnchor)
		<-done
		if result.err != nil {
			var pathErr *os.PathError
			if errors.As(result.err, &pathErr) {
				return nil, &os.PathError{Op: pathErr.Op, Path: path, Err: pathErr.Err}
			}
			return nil, result.err
		}
		if err := ctx.Err(); err != nil {
			_ = result.file.Close()
			return nil, err
		}
		return result.file, nil
	case <-ctx.Done():
		// Hold a read end until the blocked open and its cleanup complete.
		// If read access changed during the wait, the worker retains the
		// pinned inode and cleans up when a reader eventually connects.
		peer, err := os.OpenFile(pinned, os.O_RDONLY|unix.O_NONBLOCK, 0)
		close(releaseAnchor)
		if err == nil {
			<-done
			_ = peer.Close()
		}
		return nil, ctx.Err()
	}
}
