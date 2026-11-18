package cmd

import "testing"

func TestParseInputTargetPartialMatches(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  inputTarget
	}{
		{"user before mylist", "nicovideo.jp/mylist/3 and https://www.nicovideo.jp/user/2/video", inputTarget{targetTypeUser, "2"}},
		{"first user", "embeddednicovideo.jp/user/004tail nicovideo.jp/user/5", inputTarget{targetTypeUser, "004"}},
		{"skip invalid candidate", "nicovideo.jp/user/１ nicovideo.jp/user/nicovideo.jp/user/7", inputTarget{targetTypeUser, "7"}},
		{"mylist fallback", "nicovideo.jp/user/ nicovideo.jp/mylist/12 nicovideo.jp/mylist/34", inputTarget{targetTypeMylist, "12"}},
		{"user digit limit", "nicovideo.jp/user/1234567890", inputTarget{targetTypeUser, "123456789"}},
		{"mylist digit limit", "nicovideo.jp/mylist/1234567890123", inputTarget{targetTypeMylist, "123456789012"}},
		{"invalid", "nicovideo.jp/user/ nicovideo.jp/mylist/٣ NICOVIDEO.JP/user/1", inputTarget{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := parseInputTarget(tt.input)
			if target != tt.want || ok != (tt.want != inputTarget{}) {
				t.Fatalf("parseInputTarget(%q) = (%+v, %t), want %+v", tt.input, target, ok, tt.want)
			}
		})
	}
}

func BenchmarkParseInputTarget(b *testing.B) {
	for _, input := range []struct {
		name  string
		value string
	}{
		{"User", "https://www.nicovideo.jp/user/12345678/video"},
		{"Mylist", "https://www.nicovideo.jp/mylist/12345678"},
		{"Mixed", "nicovideo.jp/user/invalid nicovideo.jp/mylist/12 nicovideo.jp/user/34"},
		{"Invalid", "https://www.nicovideo.jp/watch/sm12345678"},
	} {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				parseInputTarget(input.value)
			}
		})
	}
}
