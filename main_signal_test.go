//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const signalTestHelperEnv = "GO_NICO_LIST_SIGNAL_TEST_HELPER"
const signalTestStartGateEnv = "GO_NICO_LIST_SIGNAL_TEST_START_GATE"

func TestMain(m *testing.M) {
	if runLogFIFOHelper() {
		return
	}
	os.Exit(m.Run())
}

func TestCLISignalHelper(t *testing.T) {
	if os.Getenv(signalTestHelperEnv) != "1" {
		return
	}
	if os.Getenv(signalTestStartGateEnv) == "1" {
		var ready [1]byte
		if _, err := io.ReadFull(os.Stdin, ready[:]); err != nil {
			t.Fatal(err)
		}
	}
	os.Args = append([]string{"go-nico-list"}, flag.Args()...)
	main()
	os.Exit(0)
}

func TestSIGTERMWithBlockedInput(t *testing.T) {
	for _, mode := range []string{"sorted", "no-sort", "json"} {
		for _, source := range []string{"stdin", "fifo"} {
			t.Run(mode+"/"+source, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := []string{"-test.run=^TestCLISignalHelper$", "--", "--no-progress", "invalid"}
				if mode != "sorted" {
					args = append(args, "--"+mode)
				}
				if source == "stdin" {
					args = append(args, "--stdin")
				} else {
					path := filepath.Join(t.TempDir(), "inputs")
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--input-file", path)
				}
				child := exec.CommandContext(ctx, os.Args[0], args...)
				child.Env = append(os.Environ(), signalTestHelperEnv+"=1")
				var out bytes.Buffer
				child.Stdout = &out
				if source == "stdin" {
					stdin, err := child.StdinPipe()
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = stdin.Close() }()
				}
				stderr, err := child.StderrPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					_ = child.Process.Kill()
					_ = child.Wait()
				}()
				logs := bufio.NewReader(stderr)
				// The warning confirms signal registration and that input handling has started.
				if line, err := logs.ReadString('\n'); err != nil || !strings.Contains(line, "invalid input") {
					t.Fatalf("startup log = %q, err=%v", line, err)
				}
				if err := child.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				remaining, err := io.ReadAll(logs)
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Wait(); err != nil {
					t.Fatalf("SIGTERM did not finish cleanly: %v, stderr=%s", err, remaining)
				}
				if !strings.Contains(string(remaining), "summary inputs=1 valid=0 invalid=1") {
					t.Fatalf("missing summary after SIGTERM: %s", remaining)
				}
				if mode == "json" && !strings.Contains(out.String(), `"items":[]`) {
					t.Fatalf("missing JSON output after SIGTERM: %q", out.String())
				}
			})
		}
	}
}
