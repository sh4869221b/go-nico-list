/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/spf13/cobra"
)

var (
	comment           int
	dateafter         string
	datebefore        string
	tab               bool
	url               bool
	concurrency       int = 30
	pageLimit         int
	httpClientTimeout time.Duration = defaultHTTPTimeout
	logger            *slog.Logger
	progressBarNew    func(int64, ...string) *progressbar.ProgressBar = progressbar.Default
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-nico-list",
	Short: "niconico {user}/video url get video list",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRootCmd,
}

func runRootCmd(cmd *cobra.Command, args []string) error {
	const dateFormat = "20060102"

	t, err := time.Parse(dateFormat, dateafter)
	if err != nil {
		return errors.New("dateafter format error")
	}
	afterDate := t
	t, err = time.Parse(dateFormat, datebefore)
	if err != nil {
		return errors.New("datebefore format error")
	}
	beforeDate := t

	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var idList []string
	var mu sync.Mutex

	r := regexp.MustCompile(`(((http(s)?://)?www\.)?nicovideo.jp/)?user/(?P<userID>\d{1,9})(/video)?`)
	bar := progressBarNew(int64(len(args)))

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(args))

	for i := range args {
		sem <- struct{}{}
		match := r.FindStringSubmatch(args[i])
		if len(match) == 0 {
			logger.Warn("invalid user ID", "input", args[i])
			bar.Add(1)
			<-sem
			continue
		}
		wg.Add(1)
		result := make(map[string]string)
		for j, name := range r.SubexpNames() {
			if j != 0 && name != "" {
				result[name] = match[j]
			}
		}
		userID := result["userID"]
		go func(userID string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer bar.Add(1)
			newList, err := getVideoList(ctx, userID, comment, afterDate, beforeDate, tab, url, defaultBaseURL)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			idList = append(idList, newList...)
			mu.Unlock()
			errCh <- nil
		}(userID)
	}
	wg.Wait()
	close(errCh)
	var retErr error
	for e := range errCh {
		if e != nil && retErr == nil {
			logger.Error("failed to get video list", "error", e)
			retErr = e
		}
	}
	close(sem)
	logger.Info("video list", "count", len(idList))
	NiconicoSort(idList, tab, url)
	fmt.Println(strings.Join(idList, "\n"))
	return retErr
}

const (
	tabStr             = "\t\t\t\t\t\t\t\t\t"
	urlStr             = "https://www.nicovideo.jp/watch/"
	defaultPageLimit   = 100
	defaultBaseURL     = "https://nvapi.nicovideo.jp/v3"
	defaultHTTPTimeout = 10 * time.Second
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

	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "n", 30, "number of concurrent requests")

	pageLimitDefault := defaultPageLimit
	rootCmd.Flags().IntVarP(&pageLimit, "pages", "p", pageLimitDefault, "maximum number of pages to fetch")
	info, ok := debug.ReadBuildInfo()
	if ok {
		rootCmd.Version = info.Main.Version
	}
}

// NiconicoSort sorts video IDs by their numeric part in ascending order, ignoring any preceding tab or URL strings.
func NiconicoSort(slice []string, tab bool, url bool) {
	var num = 2
	if tab {
		num += len(tabStr)
	}
	if url {
		num += len(urlStr)
	}
	str := "%08s"

	sort.Slice(slice, func(i, j int) bool {
		var s1, s2 string
		if len(slice[i]) >= num {
			s1 = slice[i][num:]
		} else {
			s1 = slice[i]
		}
		if len(slice[j]) >= num {
			s2 = slice[j][num:]
		} else {
			s2 = slice[j]
		}
		return fmt.Sprintf(str, s1) < fmt.Sprintf(str, s2)
	})
}

// GetVideoList retrieves video IDs for a user
func getVideoList(ctx context.Context, userID string, commentCount int, afterDate time.Time, beforeDate time.Time, tab bool, url bool, baseURL string) ([]string, error) {

	var resStr []string

	var beforeStr = ""
	if tab {
		beforeStr += tabStr
	}
	if url {
		beforeStr += urlStr
	}

	for i := 0; i < pageLimit; i++ {
		requestURL := fmt.Sprintf("%s/users/%s/videos?pageSize=100&page=%d", baseURL, userID, i+1)
		res, err := retriesRequest(ctx, requestURL)
		if err != nil {
			break
		}
		if res != nil {
			if res.StatusCode == http.StatusNotFound {
				break
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				logger.Error("failed to read response body", "error", err)
				return nil, err
			}

			var nicoData nicoData
			if err := json.Unmarshal(body, &nicoData); err != nil {
				logger.Error("failed to unmarshal response body", "error", err)
				return nil, err
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
	return resStr, nil
}

func retriesRequest(ctx context.Context, url string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: httpClientTimeout}

	var (
		res     *http.Response
		err     error
		retries = 100
	)
	const baseDelay = 50 * time.Millisecond
	maxRetries := retries

	for retries > 0 {
		res, err = client.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if res != nil {
					res.Body.Close()
				}
				return nil, err
			}
			if res != nil {
				res.Body.Close()
			}
		} else {
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
				break
			}
			res.Body.Close()
		}
		retries--
		wait := time.Duration(math.Min(math.Pow(2, float64(maxRetries-retries))*float64(baseDelay), float64(30*time.Second)))
		time.Sleep(wait)
	}

	if err != nil {
		return nil, err
	}

	return res, nil
}

// X-Frontend-Id: 6
// X-Frontend-Version: 0
