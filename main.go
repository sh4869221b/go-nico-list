package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/urfave/cli/v2"
)

var (
	Version = "unset"
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
			&cli.BoolFlag{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "print the version",
			},
		},
		Action: func(c *cli.Context) error {
			if c.Bool("version") {
				if Version == "unset" {
					info, ok := debug.ReadBuildInfo()
					if ok {
						Version = info.Main.Version
					}
				}
				fmt.Printf("version: %s\n", Version)
				return nil
			}
			var idList []string
			// https://www.nicovideo.jp/user/18906466/video
			r := regexp.MustCompile(`(((http(s)?://)?www\.)?nicovideo.jp/)?user/(?P<userID>\d{1,9})(/video)?`)
			idListChan := make(chan []string, c.Args().Len())
			var wg sync.WaitGroup

			for i := range c.Args().Slice() {
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
					getVideoList(userID, c.Int("comment"), c.Bool("tab"), idListChan)
					idList = append(idList, <-idListChan...)
				}()
			}
			wg.Wait()
			close(idListChan)
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
func getVideoList(userID string, commentCount int, tab bool, idListChan chan []string) {

	var resStr []string

	var tabStr = ""
	if tab {
		tabStr = "\t\t\t\t\t\t\t\t\t"
	}

	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://nvapi.nicovideo.jp/v1/users/%s/videos?pageSize=100&page=%d", userID, i+1)
		res := retriesRequest(url)
		if res != nil {
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
	}
	idListChan <- resStr
}

func retriesRequest(url string) *http.Response {
	var req *http.Request
	req, _ = http.NewRequest("GET", url, nil)
	req.Header.Set("X-Frontend-Id", "6")
	var client = new(http.Client)
	var (
		err     error
		res     *http.Response
		retries = 100
	)
	for retries > 0 {
		res, err = client.Do(req)
		if err != nil {
			retries -= 1
		} else {
			break
		}
	}
	if retries == 0 {
		log.Fatal(err)
	}

	return res
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
