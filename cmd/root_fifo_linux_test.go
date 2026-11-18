package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func waitForLogFIFOOpen(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	stacks := make([]byte, 256*1024)
	for {
		n := runtime.Stack(stacks, true)
		for _, stack := range bytes.Split(stacks[:n], []byte("\n\n")) {
			if bytes.Contains(stack, []byte("[syscall]")) && bytes.Contains(stack, []byte("cmd.openPinnedLogFIFO.func1()")) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("log writer did not enter a blocking FIFO open")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLogFIFOAcceptsShortNonblockingReader(t *testing.T) {
	path := newTestFIFO(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		file, err := openOSLogFile(ctx, path)
		if err == nil {
			err = file.Close()
		}
		done <- err
	}()
	waitForLogFIFOOpen(t)
	reader, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(reader); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("log opener missed a reader that had already closed: %v", err)
	}
}

func TestLogFIFOCancellationAfterUnlinkReleasesOpener(t *testing.T) {
	path := newTestFIFO(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		file, err := openOSLogFile(ctx, path)
		if err == nil {
			err = file.Close()
		}
		done <- err
	}()
	waitForLogFIFOOpen(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("open error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("log FIFO open did not cancel after unlink")
	}
	// An O_PATH anchor points to the deleted name until the opener exits.
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		if target, _ := os.Readlink("/proc/self/fd/" + fd.Name()); target == path+" (deleted)" {
			t.Fatalf("descriptor %s for the unlinked FIFO was left open", fd.Name())
		}
	}
}
