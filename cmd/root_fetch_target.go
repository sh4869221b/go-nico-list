package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
)

// fetchTargetListFastUnordered fetches a target using the fast unordered cap semantics.
func fetchTargetListFastUnordered(
	ctx context.Context,
	target inputTarget,
	cfg *RootConfig,
	afterDate time.Time,
	beforeDate time.Time,
	limiter *niconico.RateLimiter,
	runLogger *slog.Logger,
) ([]string, error) {
	maxVideos := maxVideosForFastUnorderedFetch(cfg)
	return fetchTargetListWithMaxVideos(ctx, target, cfg, afterDate, beforeDate, limiter, runLogger, maxVideos)
}

// maxVideosForFastUnorderedFetch returns the per-target fetch cap for fast unordered output.
func maxVideosForFastUnorderedFetch(cfg *RootConfig) int {
	if cfg.DedupeOutput && cfg.MaxVideos > 0 {
		return 0
	}
	return cfg.MaxVideos
}

// fetchTargetListWithMaxVideos fetches a target with an explicit per-target video cap.
func fetchTargetListWithMaxVideos(
	ctx context.Context,
	target inputTarget,
	cfg *RootConfig,
	afterDate time.Time,
	beforeDate time.Time,
	limiter *niconico.RateLimiter,
	runLogger *slog.Logger,
	maxVideos int,
) ([]string, error) {
	switch target.Type {
	case targetTypeUser:
		return niconico.GetVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, maxVideos, cfg.PageConcurrency, runLogger)
	case targetTypeMylist:
		return niconico.GetMylistVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, maxVideos, cfg.PageConcurrency, runLogger)
	default:
		return nil, nil
	}
}
