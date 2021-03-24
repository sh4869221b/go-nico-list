package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/mattn/natural"
	"github.com/urfave/cli/v2"
)

func main() {
	var app = &cli.App{
		Name:  "go-nico-list",
		Usage: "niconico {user}/video url get video list",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "comment",
				Value:   10,
				Aliases: []string{"c"},
				Usage:   "lower comment limit `number`",
			},
		},
		Action: func(c *cli.Context) error {
			var idList []string
			// https://www.nicovideo.jp/user/18906466/video
			for _, s := range c.Args().Slice() {
				userID := strings.Trim(s, "https://www.nicovideo.jp/user/")
				userID = strings.Trim(userID, "/video")
				idList = append(idList, getVideoList(userID, c.Int("comment"))...)
			}
			natural.Sort(idList)
			fmt.Println(strings.Join(idList[:], "\n"))
			return nil
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// GetVideoList is aaa
func getVideoList(userID string, commentCount int) []string {

	var resStr []string
	var req *http.Request

	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://nvapi.nicovideo.jp/v1/users/%s/videos?sortKey=registeredAt&pageSize=100&page=%d", userID, i+1)
		req, _ = http.NewRequest("GET", url, nil)
		req.Header.Set("X-Frontend-Id", "6")
		var client = new(http.Client)
		var res, err = client.Do(req)
		if nil != err {
			log.Fatal(err)
		}
		if res.StatusCode != 200 {
			break
		}
		body, err := ioutil.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			log.Fatal(err)
		}

		var nicoData nicoData
		if err := json.Unmarshal(body, &nicoData); err != nil {
			os.Exit(0)
		}
		if len(nicoData.Data.Items) == 0 {
			break
		}
		for _, s := range nicoData.Data.Items {
			if s.Count.Comment <= commentCount {
				continue
			}
			resStr = append(resStr, s.ID)
		}
	}
	return resStr
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
