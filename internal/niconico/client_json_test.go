package niconico

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestParsePagesJSONCompatibility(t *testing.T) {
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
				name    string
				video   string
				wantID  string
				wantErr bool
			}{
				{"mixed case", `{"ID":"sm9","COUNT":{"COMMENT":12}}`, "sm9", false},
				{"duplicate name", `{"id":"sm1","id":"sm9","count":{"comment":12}}`, "sm9", false},
				{"duplicate object merges", `{"id":"sm9","count":{"comment":12},"count":{}}`, "sm9", false},
				{"null scalar preserves value", `{"id":"sm9","id":null,"count":{"comment":12}}`, "sm9", false},
				{"invalid UTF8", "{\"id\":\"sm\xff\",\"count\":{\"comment\":12}}", "sm\ufffd", false},
				{"unknown member", `{"id":"sm9","count":{"comment":12},"future":true}`, "sm9", false},
				{"legacy timestamp", `{"id":"sm9","count":{"comment":12},"registeredAt":"2024-01-10T0:00:00Z"}`, "sm9", false},
				{"wrong scalar type", `{"id":9}`, "", true},
				{"invalid timestamp", `{"registeredAt":"invalid"}`, "", true},
				{"malformed object", `{"id":}`, "", true},
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
					if len(page.Items) != 1 || page.Items[0].ID != test.wantID || page.Items[0].CommentCount != 12 {
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
			var syntaxErr *json.SyntaxError
			if !errors.As(err, &syntaxErr) || syntaxErr.Offset != 4 {
				t.Fatalf("expected legacy trailing-data syntax error, got %v", err)
			}
			_, err = endpoint.parse([]byte(`{"meta":{"status":"invalid"}}`))
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &typeErr) || typeErr.Field != "meta.status" || typeErr.Value != "string" {
				t.Fatalf("expected legacy field type error, got %v", err)
			}
		})
	}
}
