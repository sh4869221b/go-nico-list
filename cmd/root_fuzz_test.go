package cmd

import (
	"strings"
	"testing"
)

func FuzzParseInputTargetNoPanic(f *testing.F) {
	f.Add("https://www.nicovideo.jp/user/12345/video")
	f.Add("nicovideo.jp/user/1")
	f.Add("https://www.nicovideo.jp/mylist/847130")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, input string) {
		target, ok := parseInputTarget(input)
		if !ok {
			return
		}
		if target.Type != targetTypeUser && target.Type != targetTypeMylist {
			t.Fatalf("unexpected target type %q", target.Type)
		}
		if target.ID == "" || !strings.Contains(input, "/"+target.Type+"/"+target.ID) || strings.Trim(target.ID, "0123456789") != "" {
			t.Fatalf("invalid parsed target %+v for %q", target, input)
		}
	})
}
