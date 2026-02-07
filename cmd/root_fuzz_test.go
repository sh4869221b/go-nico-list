package cmd

import (
	"regexp"
	"testing"
)

func FuzzUserIDFromMatchNoPanic(f *testing.F) {
	re := regexp.MustCompile(`((http(s)?://)?(www\.)?)nicovideo\.jp/user/(?P<userID>\d{1,9})(/video)?`)
	f.Add("https://www.nicovideo.jp/user/12345/video")
	f.Add("nicovideo.jp/user/1")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, input string) {
		match := re.FindStringSubmatch(input)
		_ = userIDFromMatch(match, re)
	})
}
