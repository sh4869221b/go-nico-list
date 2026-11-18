//go:build !linux && !darwin

package cmd

import (
	"context"
	"io"
	"os"
)

func openOSInputFile(_ context.Context, path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func openOSLogFile(_ context.Context, path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
