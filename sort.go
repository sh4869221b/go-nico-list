package main

import (
	"fmt"
	"sort"
)

//NiconicoSort is sort niconico id asc
func NiconicoSort(slice []string, tab bool) {
	var num = 2
	if tab {
		num = 11
	}
	str := "%08s"

	sort.Slice(slice, func(i, j int) bool { return fmt.Sprintf(str, slice[i][num:]) < fmt.Sprintf(str, slice[j][num:]) })
}
