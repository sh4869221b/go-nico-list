package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "go-nico-list",
		Usage: "niconico {user}/video url get video list",
		Action: func(c *cli.Context) error {
			// https://www.nicovideo.jp/user/18906466/video
			userID := strings.Trim(c.Args().First(), "https://www.nicovideo.jp/user/")
			userID = strings.Trim(userID, "/video")
			fmt.Println(GetVideoList(userID))
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
