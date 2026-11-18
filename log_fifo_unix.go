//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/sh4869221b/go-nico-list/internal/logfile"
	"golang.org/x/sys/unix"
)

const logFIFOHelperArg = "--internal-open-log-fifo"

func cliLogFileOpener(ctx context.Context) func(string) (io.WriteCloser, error) {
	return func(path string) (io.WriteCloser, error) {
		return logfile.Open(ctx, path, openLogFIFOInChild)
	}
}

// The CLI can re-execute itself to perform a blocking open. A cancellation
// kills the pending writer without ever opening a reader on the FIFO.
func openLogFIFOInChild(ctx context.Context, path string, expected os.FileInfo) (io.WriteCloser, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, logfile.ErrHelperUnavailable
	}
	parent, peer, err := makeLogFIFOSocketPair()
	if err != nil {
		return nil, logfile.ErrHelperUnavailable
	}
	defer func() { _ = peer.Close() }()
	sock, err := logFIFOConnection(parent)
	_ = parent.Close()
	if err != nil {
		return nil, logfile.ErrHelperUnavailable
	}
	defer func() { _ = sock.Close() }()

	child := exec.CommandContext(ctx, executable, logFIFOHelperArg, path)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Stdout = peer
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, logfile.ErrHelperUnavailable
	}
	_ = peer.Close() // Only the child should hold this end while receiving.

	file, receiveErr := receiveLogFIFO(sock, path)
	waitErr := child.Wait()
	if err := ctx.Err(); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, err
	}
	if waitErr != nil {
		if file != nil {
			_ = file.Close()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("open log FIFO: %s", detail)
		}
		return nil, fmt.Errorf("open log FIFO %q: %w", path, waitErr)
	}
	if receiveErr != nil {
		return nil, &os.PathError{Op: "receive log FIFO", Path: path, Err: receiveErr}
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(expected, info) {
		_ = file.Close()
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("log FIFO changed during open")}
	}
	return file, nil
}

func makeLogFIFOSocketPair() (*os.File, *os.File, error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	return os.NewFile(uintptr(fds[0]), "log FIFO socket"), os.NewFile(uintptr(fds[1]), "log FIFO child socket"), nil
}

func logFIFOConnection(file *os.File) (*net.UnixConn, error) {
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	sock, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("expected a Unix socket")
	}
	return sock, nil
}

func receiveLogFIFO(sock *net.UnixConn, path string) (*os.File, error) {
	var data [1]byte
	control := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := sock.ReadMsgUnix(data[:], control)
	if err != nil {
		return nil, err
	}
	messages, err := unix.ParseSocketControlMessage(control[:oobn])
	if err != nil {
		return nil, err
	}
	var descriptors []int
	for _, message := range messages {
		fds, err := unix.ParseUnixRights(&message)
		if err == nil {
			descriptors = append(descriptors, fds...)
		}
	}
	if n != 1 || flags&unix.MSG_CTRUNC != 0 || len(descriptors) != 1 {
		for _, fd := range descriptors {
			_ = unix.Close(fd)
		}
		return nil, errors.New("invalid log FIFO descriptor reply")
	}
	fd := descriptors[0]
	if runtime.GOOS == "linux" {
		// Let os.File use the runtime poller for blocking-style FIFO writes.
		if err := unix.SetNonblock(fd, true); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func runLogFIFOHelper() bool {
	if len(os.Args) != 3 || os.Args[1] != logFIFOHelperArg {
		return false
	}
	if err := sendLogFIFO(os.Args[2]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

func sendLogFIFO(path string) error {
	// The helper can transfer a writable descriptor. Require a socket
	// from the same credentials before opening anything.
	if err := checkLogFIFOPeer(int(os.Stdout.Fd())); err != nil {
		return fmt.Errorf("log FIFO helper socket: %w", err)
	}
	sock, err := logFIFOConnection(os.Stdout)
	if err != nil {
		return err
	}
	_ = os.Stdout.Close()
	defer func() { _ = sock.Close() }()
	go func() {
		// A parent which disappears (including on SIGKILL) closes the
		// socket, so its pending writer cannot become orphaned.
		var unexpected [1]byte
		if _, err := sock.Read(unexpected[:]); !errors.Is(err, net.ErrClosed) {
			os.Exit(1)
		}
	}()
	expected, err := os.Stat(path)
	if err != nil {
		return err
	}
	if expected.Mode()&os.ModeNamedPipe == 0 {
		return &os.PathError{Op: "open", Path: path, Err: errors.New("expected a named pipe")}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(expected, info) {
		return &os.PathError{Op: "open", Path: path, Err: errors.New("log FIFO changed during open")}
	}
	_, _, err = sock.WriteMsgUnix([]byte{0}, unix.UnixRights(int(file.Fd())), nil)
	return err
}
