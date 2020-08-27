package main

import (
	"flag"
	"fmt"
	"strings"
)

func main() {
	flag.Parse()
	args := flag.Args()

	// https://www.nicovideo.jp/user/18906466/video
	userID := strings.Trim(args[0], "https://www.nicovideo.jp/user/")
	userID = strings.Trim(userID, "/video")
	fmt.Println(GetVideoList(userID))
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
