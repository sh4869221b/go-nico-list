/*
Copyright © 2024 sh4869221b <sh4869221b@gmail.com>
*/
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/sh4869221b/go-nico-list/cmd"
)

var (
	Version = "unset"
)

func main() {
	if Version == "unset" {
		info, ok := debug.ReadBuildInfo()
		if ok {
			Version = info.Main.Version
		}
	}
	cmd.Version = Version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd.ExecuteContext(ctx)
}
