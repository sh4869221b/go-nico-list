/*
Copyright (c) 2024 sh4869221b <sh4869221b@gmail.com>
*/
package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

var Version = "unset"

func ExecuteContext(ctx context.Context) {
	cmd := NewRootCommand(RootConfig{}, RootDeps{})
	cmd.SetContext(ctx)
	cobra.CheckErr(cmd.Execute())
}
