package niconico

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNicoDataContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "nvapi_user_videos_page1.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var payload NicoData
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if payload.Meta.Status != http.StatusOK {
		t.Fatalf("meta.status: got %d, want %d", payload.Meta.Status, http.StatusOK)
	}
	if payload.Data.TotalCount != 2 {
		t.Fatalf("data.totalCount: got %d, want 2", payload.Data.TotalCount)
	}
	if len(payload.Data.Items) != 2 {
		t.Fatalf("data.items length: got %d, want 2", len(payload.Data.Items))
	}

	first := payload.Data.Items[0].Essential
	if first.Type != "video" {
		t.Fatalf("first type: got %q, want %q", first.Type, "video")
	}
	if first.ID != "sm9" {
		t.Fatalf("first id: got %q, want %q", first.ID, "sm9")
	}
	if first.Title != "sample title 1" {
		t.Fatalf("first title: got %q, want %q", first.Title, "sample title 1")
	}
	if first.Count.View != 100 {
		t.Fatalf("first view count: got %d, want 100", first.Count.View)
	}
	if first.Count.Comment != 12 {
		t.Fatalf("first comment count: got %d, want 12", first.Count.Comment)
	}
	if first.Count.Mylist != 3 {
		t.Fatalf("first mylist count: got %d, want 3", first.Count.Mylist)
	}
	if first.Count.Like != 8 {
		t.Fatalf("first like count: got %d, want 8", first.Count.Like)
	}
	wantFirstRegisteredAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if !first.RegisteredAt.Equal(wantFirstRegisteredAt) {
		t.Fatalf("first registeredAt: got %s, want %s", first.RegisteredAt.Format(time.RFC3339), wantFirstRegisteredAt.Format(time.RFC3339))
	}
	if first.Owner.OwnerType != "user" || first.Owner.ID != "1" || first.Owner.Name != "owner-1" {
		t.Fatalf("unexpected first owner payload: %+v", first.Owner)
	}
	if first.Owner.IconURL != "https://example.invalid/icon1.png" {
		t.Fatalf("first owner icon url: got %q, want %q", first.Owner.IconURL, "https://example.invalid/icon1.png")
	}
	if first.Thumbnail.URL != "https://example.invalid/thumb1.jpg" {
		t.Fatalf("first thumbnail url: got %q, want %q", first.Thumbnail.URL, "https://example.invalid/thumb1.jpg")
	}
	if first.Thumbnail.MiddleURL != "https://example.invalid/thumb1-m.jpg" {
		t.Fatalf("first thumbnail middleUrl: got %q, want %q", first.Thumbnail.MiddleURL, "https://example.invalid/thumb1-m.jpg")
	}
	if first.Thumbnail.LargeURL != "https://example.invalid/thumb1-l.jpg" {
		t.Fatalf("first thumbnail largeUrl: got %q, want %q", first.Thumbnail.LargeURL, "https://example.invalid/thumb1-l.jpg")
	}
	if first.Thumbnail.ListingURL != "https://example.invalid/thumb1-s.jpg" {
		t.Fatalf("first thumbnail listingUrl: got %q, want %q", first.Thumbnail.ListingURL, "https://example.invalid/thumb1-s.jpg")
	}
	if first.Thumbnail.NHdURL != "https://example.invalid/thumb1-hd.jpg" {
		t.Fatalf("first thumbnail nHdUrl: got %q, want %q", first.Thumbnail.NHdURL, "https://example.invalid/thumb1-hd.jpg")
	}
	if first.Duration != 120 {
		t.Fatalf("first duration: got %d, want 120", first.Duration)
	}
	if first.ShortDescription != "desc" || first.LatestCommentSummary != "summary" {
		t.Fatalf("unexpected first text fields: short=%q summary=%q", first.ShortDescription, first.LatestCommentSummary)
	}
	if first.IsChannelVideo || first.IsPaymentRequired || first.RequireSensitiveMasking {
		t.Fatalf("unexpected first boolean flags: channel=%t payment=%t masking=%t", first.IsChannelVideo, first.IsPaymentRequired, first.RequireSensitiveMasking)
	}
	if first.PlaybackPosition != nil || first.VideoLive != nil {
		t.Fatalf("expected first playback/videoLive to be nil, got %#v %#v", first.PlaybackPosition, first.VideoLive)
	}
	if first.NineD091F87 || first.Acf68865 {
		t.Fatalf("unexpected first feature flags: 9d091f87=%t acf68865=%t", first.NineD091F87, first.Acf68865)
	}

	second := payload.Data.Items[1].Essential
	if second.Type != "video" {
		t.Fatalf("second type: got %q, want %q", second.Type, "video")
	}
	if second.ID != "sm10" {
		t.Fatalf("second id: got %q, want %q", second.ID, "sm10")
	}
	if second.Title != "sample title 2" {
		t.Fatalf("second title: got %q, want %q", second.Title, "sample title 2")
	}
	if second.Count.View != 200 || second.Count.Comment != 34 || second.Count.Mylist != 5 || second.Count.Like != 13 {
		t.Fatalf("unexpected second counts: %+v", second.Count)
	}
	wantSecondRegisteredAt := time.Date(2025, 1, 3, 4, 5, 6, 0, time.UTC)
	if !second.RegisteredAt.Equal(wantSecondRegisteredAt) {
		t.Fatalf("second registeredAt: got %s, want %s", second.RegisteredAt.Format(time.RFC3339), wantSecondRegisteredAt.Format(time.RFC3339))
	}
	if second.Owner.OwnerType != "user" || second.Owner.ID != "2" || second.Owner.Name != "owner-2" {
		t.Fatalf("unexpected second owner payload: %+v", second.Owner)
	}
	if second.Owner.IconURL != "https://example.invalid/icon2.png" {
		t.Fatalf("second owner icon url: got %q, want %q", second.Owner.IconURL, "https://example.invalid/icon2.png")
	}
	if second.Thumbnail.URL != "https://example.invalid/thumb2.jpg" {
		t.Fatalf("second thumbnail url: got %q, want %q", second.Thumbnail.URL, "https://example.invalid/thumb2.jpg")
	}
	if second.Thumbnail.MiddleURL != "https://example.invalid/thumb2-m.jpg" {
		t.Fatalf("second thumbnail middleUrl: got %q, want %q", second.Thumbnail.MiddleURL, "https://example.invalid/thumb2-m.jpg")
	}
	if second.Thumbnail.LargeURL != "https://example.invalid/thumb2-l.jpg" {
		t.Fatalf("second thumbnail largeUrl: got %q, want %q", second.Thumbnail.LargeURL, "https://example.invalid/thumb2-l.jpg")
	}
	if second.Thumbnail.ListingURL != "https://example.invalid/thumb2-s.jpg" {
		t.Fatalf("second thumbnail listingUrl: got %q, want %q", second.Thumbnail.ListingURL, "https://example.invalid/thumb2-s.jpg")
	}
	if second.Duration != 240 {
		t.Fatalf("second duration: got %d, want 240", second.Duration)
	}
	if second.ShortDescription != "desc-2" || second.LatestCommentSummary != "summary-2" {
		t.Fatalf("unexpected second text fields: short=%q summary=%q", second.ShortDescription, second.LatestCommentSummary)
	}
	if second.IsChannelVideo || second.IsPaymentRequired || second.RequireSensitiveMasking {
		t.Fatalf("unexpected second boolean flags: channel=%t payment=%t masking=%t", second.IsChannelVideo, second.IsPaymentRequired, second.RequireSensitiveMasking)
	}
	if second.PlaybackPosition != nil || second.VideoLive != nil {
		t.Fatalf("expected second playback/videoLive to be nil, got %#v %#v", second.PlaybackPosition, second.VideoLive)
	}
	if second.NineD091F87 || second.Acf68865 {
		t.Fatalf("unexpected second feature flags: 9d091f87=%t acf68865=%t", second.NineD091F87, second.Acf68865)
	}
}
