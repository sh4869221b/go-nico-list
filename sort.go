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
	sort.Slice(slice, func(i, j int) bool { return fmt.Sprintf("%08s", slice[i][num:]) < fmt.Sprintf("%08s", slice[j][num:]) })
}
