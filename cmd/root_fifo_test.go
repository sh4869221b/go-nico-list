//go:build linux || darwin

package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func newTestFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCanceledFIFOInputReleasesItsDescriptor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor inspection requires Linux procfs")
	}
	path := newTestFIFO(t)
	isOpen := func() bool {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if name, err := os.Readlink("/proc/self/fd/" + entry.Name()); err == nil && name == path {
				return true
			}
		}
		return false
	}
	waitForOpenState := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for isOpen() != want {
			if time.Now().After(deadline) {
				t.Fatalf("FIFO open state did not become %t", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command, _, _ := newTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
	command.SetArgs([]string{"--input-file", path, "--json"})
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	waitForOpenState(true)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled command is still waiting for FIFO input")
	}
	waitForOpenState(false)
}

func TestFIFOInputWaitsForWriterAndRecognizesEOF(t *testing.T) {
	for _, content := range []string{"", "invalid\n"} {
		t.Run(content, func(t *testing.T) {
			path := newTestFIFO(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			reader, err := openOSInputFile(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reader.Close() }()
			done := make(chan error, 1)
			go func() {
				got, err := io.ReadAll(reader)
				if string(got) != content && err == nil {
					err = errors.New("unexpected FIFO input: " + string(got))
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("read ended before a writer connected: %v", err)
			case <-time.After(2 * fifoWait):
			}
			peer, err := os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(peer, content); err != nil {
				_ = peer.Close()
				t.Fatal(err)
			}
			if err := peer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLogFIFOCanConnectOrBeCanceled(t *testing.T) {
	for _, connect := range []bool{false, true} {
		name := "cancel"
		if connect {
			name = "connect"
		}
		t.Run(name, func(t *testing.T) {
			path := newTestFIFO(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			command, _, _ := newTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
			command.SetArgs([]string{"--logfile", path, "invalid"})
			done := make(chan error, 1)
			go func() { done <- command.ExecuteContext(ctx) }()
			select {
			case err := <-done:
				t.Fatalf("log open finished before a reader connected: %v", err)
			case <-time.After(2 * fifoWait):
			}
			var peer *os.File
			if connect {
				var err error
				peer, err = os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = peer.Close() }()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("log open did not unblock")
			}
			if peer != nil {
				got, err := io.ReadAll(peer)
				if err != nil || !strings.Contains(string(got), "invalid input") {
					t.Fatalf("FIFO log = %q, err=%v", got, err)
				}
			}
		})
	}
}
