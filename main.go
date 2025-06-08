/*
Copyright © 2024 sh4869221b <sh4869221b@gmail.com>
*/
package main

import (
	"runtime/debug"

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
	cmd.Execute()
}
