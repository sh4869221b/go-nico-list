package niconico

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParsePagesJSONV2(t *testing.T) {
	for _, endpoint := range []struct {
		name  string
		parse parsePageFunc
		wrap  string
	}{
		{"user", parseUserVideoPage, `{"data":{"items":[{"essential":%s}]}}`},
		{"mylist", parseMylistPage, `{"data":{"mylist":{"items":[{"video":%s}]}}}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, test := range []struct {
				name        string
				video       string
				wantID      string
				wantComment int
				wantErr     bool
			}{
				{"mixed case ignored", `{"ID":"sm9","COUNT":{"COMMENT":12}}`, "", 0, false},
				{"duplicate name", `{"id":"sm1","id":"sm9","count":{"comment":12}}`, "", 12, true},
				{"duplicate object", `{"id":"sm9","count":{"comment":12},"count":{}}`, "", 12, true},
				{"null scalar", `{"id":null,"count":{"comment":12}}`, "", 12, false},
				{"invalid UTF8", "{\"id\":\"sm\xff\",\"count\":{\"comment\":12}}", "", 12, true},
				{"unknown member", `{"id":"sm9","count":{"comment":12},"future":true}`, "sm9", 12, false},
				{"single-digit hour rejected", `{"id":"sm9","count":{"comment":12},"registeredAt":"2024-01-10T0:00:00Z"}`, "", 12, true},
				{"valid timestamp", `{"id":"sm9","count":{"comment":12},"registeredAt":"2024-01-10T00:00:00Z"}`, "sm9", 12, false},
				{"wrong scalar type", `{"id":9}`, "", 12, true},
				{"invalid timestamp", `{"registeredAt":"invalid"}`, "", 12, true},
				{"malformed object", `{"id":}`, "", 12, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					page, err := endpoint.parse([]byte(fmt.Sprintf(endpoint.wrap, test.video)))
					if test.wantErr {
						if err == nil {
							t.Fatal("expected decode error")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(page.Items) != 1 || page.Items[0].ID != test.wantID || page.Items[0].CommentCount != test.wantComment {
						t.Fatalf("unexpected items: %+v", page.Items)
					}
				})
			}
			for _, body := range []string{`null`, `{"data":null}`, `{"data":{"items":null,"mylist":{"items":null}}}`, `{"data":{"items":[],"mylist":{"items":[]}}}`} {
				page, err := endpoint.parse([]byte(body))
				if err != nil || len(page.Items) != 0 || page.TotalCount != nil {
					t.Fatalf("unexpected empty page for %s: %+v, %v", body, page, err)
				}
			}
			_, err := endpoint.parse([]byte(`{} {}`))
			var syntaxErr *jsontext.SyntacticError
			if !errors.As(err, &syntaxErr) {
				t.Fatalf("expected v2 trailing-data syntax error, got %v", err)
			}
			_, err = endpoint.parse([]byte(`{"meta":{"status":"invalid"}}`))
			var typeErr *json.SemanticError
			if !errors.As(err, &typeErr) || typeErr.JSONPointer != "/meta/status" || typeErr.JSONKind != '"' {
				t.Fatalf("expected v2 field type error, got %v", err)
			}
		})
	}
}

func TestParseLargeUserPageJSONV2(t *testing.T) {
	const item = `{"essential":{"id":"sm9","count":{"comment":12},"registeredAt":"2024-01-10T00:00:00Z"}}`
	const prefix = `{"data":{"items":[`
	const suffix = `]}}`

	body := []byte(prefix + strings.Repeat(item+",", pageSize) + item + suffix)
	page, err := parseUserVideoPage(body)
	if err != nil || len(page.Items) != pageSize+1 {
		t.Fatalf("decode large page: %d items, %v", len(page.Items), err)
	}
	body = []byte(prefix + strings.Repeat(item+",", pageSize) + `{"essential":{"owner":{"name":9}}}` + suffix)
	_, err = parseUserVideoPage(body)
	var typeErr *json.SemanticError
	wantPointer := jsontext.Pointer(fmt.Sprintf("/data/items/%d/essential/owner/name", pageSize))
	if !errors.As(err, &typeErr) || typeErr.JSONPointer != wantPointer {
		t.Fatalf("expected v2 field type error at %s, got %v", wantPointer, err)
	}

	body = []byte(prefix + strings.Repeat(" ", minPooledPageBytes) + `{}` + suffix)
	empty, err := parseUserVideoPage(body)
	if err != nil || len(empty.Items) != 1 || empty.Items[0] != (videoItem{}) {
		t.Fatalf("decode sparse page: %+v, %v", empty.Items, err)
	}
	for i, got := range page.Items {
		if got.ID != "sm9" || got.CommentCount != 12 || got.RegisteredAt.IsZero() {
			t.Fatalf("item %d after later decodes: %+v", i, got)
		}
	}
}

func TestParseLargeMylistPageJSONV2(t *testing.T) {
	const item = `{"video":{"id":"sm9","count":{"comment":12},"registeredAt":"2024-01-10T00:00:00Z"}}`
	const prefix = `{"data":{"mylist":{"totalItemCount":120,"items":[`
	const suffix = `]}}}`
	items := strings.Repeat(item+",", pageSize)

	body := []byte(prefix + items + item + suffix)
	page, err := parseMylistPage(body)
	if err != nil || len(page.Items) != pageSize+1 || page.TotalCount == nil || *page.TotalCount != 120 {
		t.Fatalf("decode large mylist: %d items, total %v, %v", len(page.Items), page.TotalCount, err)
	}
	body = []byte(prefix + items + `{"video":{"count":{"comment":"invalid"}}}` + suffix)
	_, err = parseMylistPage(body)
	var typeErr *json.SemanticError
	wantPointer := jsontext.Pointer(fmt.Sprintf("/data/mylist/items/%d/video/count/comment", pageSize))
	if !errors.As(err, &typeErr) || typeErr.JSONPointer != wantPointer {
		t.Fatalf("expected v2 field type error at %s, got %v", wantPointer, err)
	}
}
