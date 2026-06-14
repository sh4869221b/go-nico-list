package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
)

func fetchTargetList(
	ctx context.Context,
	target inputTarget,
	cfg *RootConfig,
	afterDate time.Time,
	beforeDate time.Time,
	limiter *niconico.RateLimiter,
	runLogger *slog.Logger,
) ([]string, error) {
	switch target.Type {
	case targetTypeUser:
		return niconico.GetVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, cfg.PageConcurrency, runLogger)
	case targetTypeMylist:
		return niconico.GetMylistVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, cfg.PageConcurrency, runLogger)
	default:
		return nil, nil
	}
}
