package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
)

var (
	Version = "unset"
)

func main() {
	if Version == "unset" {
		info, ok := debug.ReadBuildInfo()
		if ok {
			Version = info.Main.Version
		}
	}
	var app = &cli.App{
		Name:    "go-nico-list",
		Usage:   "niconico {user}/video url get video list",
		Version: Version,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "comment",
				Value:   10,
				Aliases: []string{"c"},
				Usage:   "lower comment limit `number`",
			},
			&cli.StringFlag{
				Name:    "date",
				Value:   "10000101",
				Aliases: []string{"d"},
				Usage:   "date `YYYYMMDD`",
			},
			&cli.BoolFlag{
				Name:    "tab",
				Aliases: []string{"t"},
				Usage:   "id tab Separated flag",
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() == 0 {
				fmt.Println("Please input userID")
				return nil
			}

			var dateFormt = "20060102"

			var t, err = time.Parse(dateFormt, c.String("date"))
			if err != nil {
				t, _ = time.Parse(dateFormt, "")
			}

			var beforeDate = t
			var idList []string
			// https://www.nicovideo.jp/user/18906466/video
			r := regexp.MustCompile(`(((http(s)?://)?www\.)?nicovideo.jp/)?user/(?P<userID>\d{1,9})(/video)?`)
			idListChan := make(chan []string, c.Args().Len())
			sem := make(chan struct{}, 30) // concurrency数のバッファ
			var wg sync.WaitGroup

			for i := range c.Args().Slice() {
				sem <- struct{}{}
				wg.Add(1)
				match := r.FindStringSubmatch(c.Args().Get(i))
				result := make(map[string]string)
				for j, name := range r.SubexpNames() {
					if j != 0 && name != "" {
						result[name] = match[j]
					}
				}
				userID := result["userID"]
				go func() {
					defer wg.Done()
					defer func() { <-sem }() // 処理が終わったらチャネルを解放
					getVideoList(userID, c.Int("comment"), beforeDate, c.Bool("tab"), idListChan)
					idList = append(idList, <-idListChan...)
				}()
			}
			wg.Wait()
			close(idListChan)
			// natural.Sort(idList
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
func getVideoList(userID string, commentCount int, beforeDate time.Time, tab bool, idListChan chan []string) {

	var resStr []string

	var tabStr = ""
	if tab {
		tabStr = "\t\t\t\t\t\t\t\t\t"
	}

	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://nvapi.nicovideo.jp/v3/users/%s/videos?pageSize=100&page=%d", userID, i+1)
		res := retriesRequest(url)
		if res != nil {
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				log.Fatal(err)
				os.Exit(0)
			}

			var nicoData nicoData
			if err := json.Unmarshal(body, &nicoData); err != nil {
				log.Fatal(err)
				os.Exit(0)
			}
			if len(nicoData.Data.Items) == 0 {
				break
			}
			for _, s := range nicoData.Data.Items {
				if s.Essential.Count.Comment <= commentCount {
					continue
				}
				if s.Essential.RegisteredAt.Before(beforeDate) {
					continue
				}
				resStr = append(resStr, fmt.Sprintf("%s%s", tabStr, s.Essential.ID))
			}
		}
	}
	idListChan <- resStr
}

func retriesRequest(url string) *http.Response {
	var req *http.Request
	req, _ = http.NewRequest("GET", url, nil)
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	var client = new(http.Client)
	var (
		err     error
		res     *http.Response
		retries = 100
	)
	for retries > 0 {
		res, err = client.Do(req)
		if err != nil || res.StatusCode != 200 {
			retries -= 1
		} else {
			break
		}
	}
	if retries == 0 {
		log.Fatal(err)
		os.Exit(0)
	}

	return res
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
