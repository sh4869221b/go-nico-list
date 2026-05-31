/*
Copyright (c) 2024 sh4869221b <sh4869221b@gmail.com>
*/
package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

var Version = "unset"

const (
	defaultBaseURL     = "https://nvapi.nicovideo.jp/v3"
	defaultHTTPTimeout = 10 * time.Second
	defaultRetries     = 10
)

func Execute() {
	ExecuteContext(context.Background())
}

func ExecuteContext(ctx context.Context) {
	cmd := NewRootCommand(DefaultConfig(), DefaultDeps())
	cmd.SetContext(ctx)
	cobra.CheckErr(cmd.Execute())
}
