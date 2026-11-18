//go:build !linux && !darwin

package cmd

import (
	"context"
	"io"
	"os"
)

func openOSInputFile(_ context.Context, path string, detach func()) (io.ReadCloser, error) {
	detach()
	return os.Open(path)
}
