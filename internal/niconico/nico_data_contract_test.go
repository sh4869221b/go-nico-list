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
	if first.ID != "sm9" {
		t.Fatalf("first id: got %q, want %q", first.ID, "sm9")
	}
	if first.Count.Comment != 12 {
		t.Fatalf("first comment count: got %d, want 12", first.Count.Comment)
	}
	wantFirstRegisteredAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if !first.RegisteredAt.Equal(wantFirstRegisteredAt) {
		t.Fatalf("first registeredAt: got %s, want %s", first.RegisteredAt.Format(time.RFC3339), wantFirstRegisteredAt.Format(time.RFC3339))
	}
}
