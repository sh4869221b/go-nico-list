package niconico

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestNicoDataContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/nvapi_user_videos_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload NicoData
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Meta.Status != http.StatusOK {
		t.Fatalf("meta.status: got %d, want %d", payload.Meta.Status, http.StatusOK)
	}
	if payload.Data.TotalCount == nil || *payload.Data.TotalCount != 2 {
		t.Fatalf("data.totalCount: got %v, want 2", payload.Data.TotalCount)
	}
	if len(payload.Data.Items) != 2 {
		t.Fatalf("data.items length: got %d, want 2", len(payload.Data.Items))
	}

	for i, want := range []struct {
		id           string
		counts       [4]int
		registeredAt time.Time
		duration     int
		textSuffix   string
	}{
		{"sm9", [4]int{100, 12, 3, 8}, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), 120, ""},
		{"sm10", [4]int{200, 34, 5, 13}, time.Date(2025, 1, 3, 4, 5, 6, 0, time.UTC), 240, "-2"},
	} {
		t.Run(want.id, func(t *testing.T) {
			got := payload.Data.Items[i].Essential
			if got.Type != "video" || got.ID != want.id || got.Title != fmt.Sprintf("sample title %d", i+1) {
				t.Fatalf("identity fields: type=%q id=%q title=%q", got.Type, got.ID, got.Title)
			}
			counts := [4]int{got.Count.View, got.Count.Comment, got.Count.Mylist, got.Count.Like}
			if counts != want.counts {
				t.Fatalf("counts: got %v, want %v", counts, want.counts)
			}
			if !got.RegisteredAt.Equal(want.registeredAt) {
				t.Fatalf("registeredAt: got %s, want %s", got.RegisteredAt, want.registeredAt)
			}
			owner := [4]string{got.Owner.OwnerType, got.Owner.ID, got.Owner.Name, got.Owner.IconURL}
			wantOwner := [4]string{"user", fmt.Sprint(i + 1), fmt.Sprintf("owner-%d", i+1), fmt.Sprintf("https://example.invalid/icon%d.png", i+1)}
			if owner != wantOwner {
				t.Fatalf("owner: got %v, want %v", owner, wantOwner)
			}
			thumbnail := [5]string{got.Thumbnail.URL, got.Thumbnail.MiddleURL, got.Thumbnail.LargeURL, got.Thumbnail.ListingURL, got.Thumbnail.NHdURL}
			prefix := fmt.Sprintf("https://example.invalid/thumb%d", i+1)
			wantThumbnail := [5]string{prefix + ".jpg", prefix + "-m.jpg", prefix + "-l.jpg", prefix + "-s.jpg", prefix + "-hd.jpg"}
			if thumbnail != wantThumbnail {
				t.Fatalf("thumbnail: got %v, want %v", thumbnail, wantThumbnail)
			}
			if got.Duration != want.duration || got.ShortDescription != "desc"+want.textSuffix || got.LatestCommentSummary != "summary"+want.textSuffix {
				t.Fatalf("duration/text: duration=%d short=%q summary=%q", got.Duration, got.ShortDescription, got.LatestCommentSummary)
			}
			if got.IsChannelVideo || got.IsPaymentRequired || got.RequireSensitiveMasking || got.NineD091F87 || got.Acf68865 {
				t.Fatalf("unexpected boolean flags: %+v", got)
			}
			if got.PlaybackPosition != nil || got.VideoLive != nil {
				t.Fatalf("playbackPosition/videoLive: got %#v %#v, want nil", got.PlaybackPosition, got.VideoLive)
			}
		})
	}
}
