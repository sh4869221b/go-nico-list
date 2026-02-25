package cmd

import "testing"

func FuzzParseInputTargetNoPanic(f *testing.F) {
	f.Add("https://www.nicovideo.jp/user/12345/video")
	f.Add("nicovideo.jp/user/1")
	f.Add("https://www.nicovideo.jp/mylist/847130")
	f.Add("invalid")

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = parseInputTarget(input)
	})
}
