/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-nico-list",
	Short: "niconico {user}/video url get video list",
	Long:  `niconico {user}/video url get video list`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("please input userID")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {

		var dateFormt = "20060102"

		var t, err = time.Parse(dateFormt, dateafter)
		if err != nil {
			t, _ = time.Parse(dateFormt, "")
		}

		var afterDate = t

		t, err = time.Parse(dateFormt, datebefore)
		if err != nil {
			t, _ = time.Parse(dateFormt, "")
		}

		var beforeDate = t

		var idList []string
		// https://www.nicovideo.jp/user/18906466/video
		r := regexp.MustCompile(`(((http(s)?://)?www\.)?nicovideo.jp/)?user/(?P<userID>\d{1,9})(/video)?`)
		idListChan := make(chan []string, len(args))
		sem := make(chan struct{}, 30) // concurrency数のバッファ
		var wg sync.WaitGroup

		for i := range args {
			sem <- struct{}{}
			wg.Add(1)
			match := r.FindStringSubmatch(args[i])
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
				getVideoList(userID, comment, afterDate, beforeDate, tab, url, idListChan)
				idList = append(idList, <-idListChan...)
			}()
		}
		wg.Wait()
		close(idListChan)
		// natural.Sort(idList
		NiconicoSort(idList, tab, url)
		fmt.Println(strings.Join(idList[:], "\n"))
	},
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}
var (
	// comment represents the number of comments.
	comment int
	// dateafter specifies the date after which certain operations or filters should be applied.
	// It is expected to be in a valid date format.
	dateafter string
	// datebefore is a string that represents a date before a specified point in time.
	// It is used to filter or compare dates in the application.
	datebefore string
	// tab indicates whether tab completion is enabled.
	tab bool
	// url indicates whether the URL flag is set.
	url bool
)

const (
	tabStr = "\t\t\t\t\t\t\t\t\t"
	urlStr = "https://www.nicovideo.jp/watch/"
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.go-nico-list.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().IntVarP(&comment, "comment", "c", 0, "lower comment limit `number`")
	rootCmd.Flags().StringVarP(&dateafter, "dateafter", "a", "10000101", "date `YYYYMMDD` after")
	rootCmd.Flags().StringVarP(&datebefore, "datebefore", "b", "99991231", "date `YYYYMMDD` before")
	rootCmd.Flags().BoolVarP(&tab, "tab", "t", false, "id tab Separated flag")
	rootCmd.Flags().BoolVarP(&url, "url", "u", false, "output id add url")
	info, ok := debug.ReadBuildInfo()
	if ok {
		rootCmd.Version = info.Main.Version
	}
}

func NiconicoSort(slice []string, tab bool, url bool) {
	var num = 2
	if tab {
		num += len(tabStr)
	}
	if url {
		num += len(urlStr)
	}
	str := "%08s"

	sort.Slice(slice, func(i, j int) bool { return fmt.Sprintf(str, slice[i][num:]) < fmt.Sprintf(str, slice[j][num:]) })
}

// GetVideoList is aaa
func getVideoList(userID string, commentCount int, afterDate time.Time, beforeDate time.Time, tab bool, url bool, idListChan chan []string) {

	var resStr []string

	var beforeStr = ""
	if tab {
		beforeStr += tabStr
	}
	if url {
		beforeStr += urlStr
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
				if s.Essential.RegisteredAt.Before(afterDate) {
					continue
				}
				if s.Essential.RegisteredAt.After(beforeDate.AddDate(0, 0, 1)) {
					continue
				}
				resStr = append(resStr, fmt.Sprintf("%s%s", beforeStr, s.Essential.ID))
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
