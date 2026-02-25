package cmd

import "regexp"

const (
	targetTypeUser   = "user"
	targetTypeMylist = "mylist"
)

type inputTarget struct {
	Type string
	ID   string
}

var (
	userInputPattern   = regexp.MustCompile(`((http(s)?://)?(www\.)?)nicovideo\.jp/user/(?P<userID>\d{1,9})(/video)?`)
	mylistInputPattern = regexp.MustCompile(`((http(s)?://)?(www\.)?)nicovideo\.jp/mylist/(?P<mylistID>\d{1,12})`) // mylist IDs are numeric
)

func parseInputTarget(input string) (inputTarget, bool) {
	if match := userInputPattern.FindStringSubmatch(input); len(match) > 0 {
		return inputTarget{Type: targetTypeUser, ID: submatchByName(match, userInputPattern, "userID")}, true
	}
	if match := mylistInputPattern.FindStringSubmatch(input); len(match) > 0 {
		return inputTarget{Type: targetTypeMylist, ID: submatchByName(match, mylistInputPattern, "mylistID")}, true
	}
	return inputTarget{}, false
}

func submatchByName(match []string, re *regexp.Regexp, name string) string {
	idx := re.SubexpIndex(name)
	if idx < 0 || idx >= len(match) {
		return ""
	}
	return match[idx]
}
