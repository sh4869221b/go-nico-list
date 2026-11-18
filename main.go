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
	"github.com/spf13/cobra"
)

var Version = "unset"

// main resolves version info, installs signal handling, and runs the CLI.
func main() {
	if runLogFIFOHelper() {
		return
	}
	if Version == "unset" {
		info, ok := debug.ReadBuildInfo()
		if ok {
			Version = info.Main.Version
		}
	}
	cmd.Version = Version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := cmd.NewRootCommand(cmd.RootConfig{}, cmd.RootDeps{OpenLogFile: cliLogFileOpener(ctx)})
	cobra.CheckErr(command.ExecuteContext(ctx))
}
