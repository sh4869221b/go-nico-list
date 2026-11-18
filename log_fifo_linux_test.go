package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fifoCLI struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan struct{}
	err    error
}

func startFIFOCLI(t *testing.T, path, input string) *fifoCLI {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	child := &fifoCLI{done: make(chan struct{})}
	child.cmd = exec.CommandContext(ctx, executable,
		"-test.run=^TestCLISignalHelper$", "--", "--json", "--logfile", path, input)
	child.cmd.Env = append(os.Environ(), signalTestHelperEnv+"=1", "GORACE=atexit_sleep_ms=0")
	child.cmd.Stdout, child.cmd.Stderr = &child.stdout, &child.stderr
	if err := child.cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() {
		child.err = child.cmd.Wait()
		close(child.done)
	}()
	t.Cleanup(func() {
		_ = child.cmd.Process.Signal(syscall.SIGTERM)
		<-child.done
		cancel()
	})
	return child
}

func (child *fifoCLI) wait(t *testing.T) {
	t.Helper()
	select {
	case <-child.done:
		if child.err != nil {
			t.Fatalf("CLI exit = %v, stderr = %q", child.err, child.stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CLI did not exit")
	}
}

func isWaitingForFIFOReader(pid int) bool {
	tasks, _ := filepath.Glob(fmt.Sprintf("/proc/%d/task/*", pid))
	for _, task := range tasks {
		wchan, _ := os.ReadFile(filepath.Join(task, "wchan"))
		if strings.Contains(string(wchan), "wait_for_partner") || strings.Contains(string(wchan), "fifo_open") {
			return true
		}
	}
	return false
}

func fifoWriterPID(pid int) int {
	if isWaitingForFIFOReader(pid) {
		return pid
	}
	entries, _ := os.ReadDir("/proc")
	parent := strconv.Itoa(pid)
	for _, entry := range entries {
		child, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		// After the parenthesized process name come state and parent PID.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) >= 2 && fields[1] == parent && isWaitingForFIFOReader(child) {
			return child
		}
	}
	return 0
}

func waitForFIFOLogWriter(t *testing.T, child *fifoCLI) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if writer := fifoWriterPID(child.cmd.Process.Pid); writer != 0 {
			return writer
		}
		select {
		case <-child.done:
			t.Fatalf("CLI left the FIFO wait early: %v, stderr = %q", child.err, child.stderr.String())
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("CLI writer did not enter the FIFO wait")
	return 0
}

func newLogFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSIGTERMCancelsOnlyItsLogFIFOWriter(t *testing.T) {
	path := newLogFIFO(t)
	canceled := startFIFOCLI(t, path, "canceled")
	survivor := startFIFOCLI(t, path, "survivor")
	waitForFIFOLogWriter(t, canceled)
	waitForFIFOLogWriter(t, survivor)

	if err := canceled.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	canceled.wait(t)
	if !strings.Contains(canceled.stderr.String(), "summary inputs=0 valid=0 invalid=0 fetch_ok=0 fetch_err=0 output_count=0\n") ||
		!strings.Contains(canceled.stdout.String(), `"items":[]`) {
		t.Fatalf("canceled CLI report stdout=%q, stderr=%q", canceled.stdout.String(), canceled.stderr.String())
	}
	waitForFIFOLogWriter(t, survivor)

	reader, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	survivor.wait(t)
	logs, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logs, []byte(`"input":"survivor"`)) || bytes.Contains(logs, []byte(`"input":"canceled"`)) {
		t.Fatalf("actual reader got logs %q", logs)
	}
}

func TestLogFIFOAcceptsShortNonblockingReader(t *testing.T) {
	path := newLogFIFO(t)
	child := startFIFOCLI(t, path, "invalid")
	waitForFIFOLogWriter(t, child)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatal(err)
	}
	child.wait(t)
	if !strings.Contains(child.stderr.String(), "summary inputs=1 valid=0 invalid=1") {
		t.Fatalf("CLI did not proceed after the reader disconnected: %q", child.stderr.String())
	}
}

func TestLogFIFOCancellationAfterUnlinkReapsWriter(t *testing.T) {
	path := newLogFIFO(t)
	child := startFIFOCLI(t, path, "invalid")
	writer := waitForFIFOLogWriter(t, child)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := child.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	child.wait(t)
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", writer)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FIFO writer process %d was not reaped: %v", writer, err)
	}
}
