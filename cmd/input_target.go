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
	userInputPattern   = regexp.MustCompile(`(?:https?://)?(?:www\.)?nicovideo\.jp/user/(?P<userID>\d{1,9})(?:/video)?`)
	mylistInputPattern = regexp.MustCompile(`(?:https?://)?(?:www\.)?nicovideo\.jp/mylist/(?P<mylistID>\d{1,12})`) // mylist IDs are numeric
)

func parseInputTarget(input string) (inputTarget, bool) {
	if match := userInputPattern.FindStringSubmatch(input); len(match) > 0 {
		return inputTarget{Type: targetTypeUser, ID: match[1]}, true
	}
	if match := mylistInputPattern.FindStringSubmatch(input); len(match) > 0 {
		return inputTarget{Type: targetTypeMylist, ID: match[1]}, true
	}
	return inputTarget{}, false
}
