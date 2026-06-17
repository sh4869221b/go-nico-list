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
	if cfg.DedupeOutput && cfg.MaxVideos > 0 {
		return fetchTargetUniqueList(ctx, target, cfg, afterDate, beforeDate, limiter, runLogger)
	}
	return fetchTargetListWithMaxVideos(ctx, target, cfg, afterDate, beforeDate, limiter, runLogger, cfg.MaxVideos)
}

// fetchTargetUniqueList fetches a target until it has enough unique filtered IDs.
func fetchTargetUniqueList(
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
		return niconico.GetUniqueVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, runLogger)
	case targetTypeMylist:
		return niconico.GetUniqueMylistVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, runLogger)
	default:
		return nil, nil
	}
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
