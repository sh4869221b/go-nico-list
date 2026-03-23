package cmd

import (
	"fmt"
	"regexp"
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
			if userInputPattern.MatchString(input) || mylistInputPattern.MatchString(input) {
				t.Fatalf("expected parser to accept %q", input)
			}
			return
		}

		switch target.Type {
		case targetTypeUser:
			match := userInputPattern.FindStringSubmatch(input)
			if len(match) == 0 {
				t.Fatalf("expected %q to match the user input pattern", input)
			}
			want := submatchByName(match, userInputPattern, "userID")
			if target.ID != want {
				t.Fatalf("expected parsed user id %q, got %q for %q", want, target.ID, input)
			}
		case targetTypeMylist:
			match := mylistInputPattern.FindStringSubmatch(input)
			if len(match) == 0 {
				t.Fatalf("expected %q to match the mylist input pattern", input)
			}
			want := submatchByName(match, mylistInputPattern, "mylistID")
			if target.ID != want {
				t.Fatalf("expected parsed mylist id %q, got %q for %q", want, target.ID, input)
			}
		default:
			t.Fatalf("unexpected target type %q", target.Type)
		}
		if target.ID == "" {
			t.Fatalf("expected parsed target to include an id for %q", input)
		}
	})
}

func FuzzSubmatchByNameNoPanic(f *testing.F) {
	f.Add(uint8(0), "userID", uint8(0))
	f.Add(uint8(0), "userID", uint8(1))
	f.Add(uint8(1), "mylistID", uint8(3))
	f.Add(uint8(2), "missing", uint8(4))

	f.Fuzz(func(t *testing.T, patternKind uint8, name string, matchLen uint8) {
		var re *regexp.Regexp
		switch patternKind % 3 {
		case 0:
			re = userInputPattern
		case 1:
			re = mylistInputPattern
		default:
			re = regexp.MustCompile(`(plain)(pattern)`)
		}

		match := make([]string, int(matchLen%16))
		for i := range match {
			match[i] = fmt.Sprintf("capture-%d", i)
		}
		got := submatchByName(match, re, name)
		idx := re.SubexpIndex(name)
		if idx < 0 || idx >= len(match) {
			if got != "" {
				t.Fatalf("expected empty submatch for idx=%d len=%d, got %q", idx, len(match), got)
			}
			return
		}
		if got != match[idx] {
			t.Fatalf("expected match[%d]=%q, got %q", idx, match[idx], got)
		}
	})
}
