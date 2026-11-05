package niconico

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
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
