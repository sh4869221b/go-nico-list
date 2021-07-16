package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

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
			&cli.BoolFlag{
				Name:    "tab",
				Aliases: []string{"t"},
				Usage:   "id tab Separated flag",
			},
		},
		Action: func(c *cli.Context) error {
			var idList []string
			// https://www.nicovideo.jp/user/18906466/video
			r := regexp.MustCompile(`(((http(s)?://)?www\.)?nicovideo.jp/)?user/(?P<userID>\d{1,9})(/video)?`)
			for _, s := range c.Args().Slice() {
				match := r.FindStringSubmatch(s)
				result := make(map[string]string)
				for i, name := range r.SubexpNames() {
					if i != 0 && name != "" {
						result[name] = match[i]
					}
				}
				userID := result["userID"]
				idList = append(idList, getVideoList(userID, c.Int("comment"), c.Bool("tab"))...)
			}
			// natural.Sort(idList)
			NiconicoSort(idList, c.Bool("tab"))
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
func getVideoList(userID string, commentCount int, tab bool) []string {

	var resStr []string
	var req *http.Request
	var tabStr = ""
	if tab {
		tabStr = "\t\t\t\t\t\t\t\t\t"
	}

	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://nvapi.nicovideo.jp/v1/users/%s/videos?pageSize=100&page=%d", userID, i+1)
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
			resStr = append(resStr, tabStr+s.ID)
		}
	}
	return resStr
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
