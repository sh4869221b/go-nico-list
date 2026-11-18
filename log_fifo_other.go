//go:build !linux

package main

import (
	"context"
	"io"
)

func runLogFIFOHelper() bool { return false }

func cliLogFileOpener(context.Context) func(string) (io.WriteCloser, error) { return nil }
