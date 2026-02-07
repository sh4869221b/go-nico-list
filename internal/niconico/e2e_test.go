//go:build e2e

package niconico

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestGetVideoListE2E(t *testing.T) {
	userID := strings.TrimSpace(os.Getenv("GO_NICO_LIST_E2E_USER_ID"))
	if userID == "" {
		t.Skip("set GO_NICO_LIST_E2E_USER_ID to run e2e test")
	}
	if !regexp.MustCompile(`^\d{1,9}$`).MatchString(userID) {
		t.Fatalf("GO_NICO_LIST_E2E_USER_ID must be 1-9 digits: %q", userID)
	}

	baseURL := strings.TrimSpace(os.Getenv("GO_NICO_LIST_E2E_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://nvapi.nicovideo.jp/v3"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ids, err := GetVideoList(
		ctx,
		userID,
		0,
		time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		false,
		false,
		baseURL,
		3,
		10*time.Second,
		nil,
		1,
		1,
		logger,
	)
	if err != nil {
		t.Fatalf("GetVideoList returned error: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("expected at least one id for user %s", userID)
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "sm") {
			t.Fatalf("unexpected video id format: %q", id)
		}
	}
}
