/*
Copyright © 2024 sh4869221b <sh4869221b@gmail.com>
*/
package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/sh4869221b/go-nico-list/internal/niconico"

	"github.com/spf13/cobra"
)

var (
	comment           int
	dateafter         string
	datebefore        string
	tab               bool
	url               bool
	concurrency       int           = 3
	retries           int           = defaultRetries
	httpClientTimeout time.Duration = defaultHTTPTimeout
	inputFilePath     string
	readStdin         bool
	logFilePath       string
	Version           = "unset"
	logger            *slog.Logger
	progressBarNew    func(int64, ...string) *progressbar.ProgressBar = progressbar.Default
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-nico-list",
	Short: "niconico {user}/video url get video list",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRootCmd,
}

func runRootCmd(cmd *cobra.Command, args []string) error {
	if concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if retries < 1 {
		return errors.New("retries must be at least 1")
	}

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
	if logFilePath != "" {
		logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer logFile.Close()
		logger = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{}))
	}
	slog.SetDefault(logger)

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}

	var idList []string
	var mu sync.Mutex
	inputs, err := collectInputs(cmd, args)
	if err != nil {
		return err
	}

	r := regexp.MustCompile(`((http(s)?://)?(www\.)?)nicovideo\.jp/user/(?P<userID>\d{1,9})(/video)?`)
	var bar *progressbar.ProgressBar
	if cmd != nil {
		bar = progressbar.NewOptions64(int64(len(inputs)), progressbar.OptionSetWriter(cmd.ErrOrStderr()))
	} else {
		bar = progressBarNew(int64(len(inputs)))
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(inputs))

	for i := range inputs {
		sem <- struct{}{}
		match := r.FindStringSubmatch(inputs[i])
		if len(match) == 0 {
			logger.Warn("invalid user ID", "input", inputs[i])
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
			newList, err := niconico.GetVideoList(ctx, userID, comment, afterDate, beforeDate, tab, url, baseURL, retries, httpClientTimeout, logger)
			mu.Lock()
			idList = append(idList, newList...)
			mu.Unlock()
			if err != nil {
				errCh <- err
				return
			}
			errCh <- nil
		}(userID)
	}
	wg.Wait()
	close(errCh)
	var retErr error
	for e := range errCh {
		if e == nil {
			continue
		}
		logger.Error("failed to get video list", "error", e)
		if retErr == nil {
			retErr = e
		}
	}
	close(sem)
	logger.Info("video list", "count", len(idList))
	if len(idList) > 0 {
		var out io.Writer = os.Stdout
		if cmd != nil {
			out = cmd.OutOrStdout()
		}
		niconico.NiconicoSort(idList, tab, url)
		fmt.Fprintln(out, strings.Join(idList, "\n"))
	}
	return retErr
}

const (
	defaultBaseURL     = "https://nvapi.nicovideo.jp/v3"
	defaultHTTPTimeout = 10 * time.Second
	defaultRetries     = 10
)

var baseURL = defaultBaseURL

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ExecuteContext(context.Background())
}

func ExecuteContext(ctx context.Context) {
	rootCmd.Version = Version
	cobra.CheckErr(rootCmd.ExecuteContext(ctx))
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
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

	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "n", 3, "number of concurrent requests")

	rootCmd.Flags().DurationVar(&httpClientTimeout, "timeout", defaultHTTPTimeout, "HTTP client timeout")
	rootCmd.Flags().IntVar(&retries, "retries", defaultRetries, "number of retries for requests")
	rootCmd.Flags().StringVar(&inputFilePath, "input-file", "", "read inputs from file (newline-separated)")
	rootCmd.Flags().BoolVar(&readStdin, "stdin", false, "read inputs from stdin (newline-separated)")
	rootCmd.Flags().StringVar(&logFilePath, "logfile", "", "log output file path")

}

func collectInputs(cmd *cobra.Command, args []string) ([]string, error) {
	inputs := make([]string, 0, len(args))
	inputs = append(inputs, args...)

	if inputFilePath != "" {
		lines, err := readLinesFromFile(inputFilePath)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, lines...)
	}

	if readStdin {
		var reader io.Reader = os.Stdin
		if cmd != nil {
			reader = cmd.InOrStdin()
		}
		lines, err := readLines(reader)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, lines...)
	}

	if len(inputs) == 0 {
		return nil, errors.New("no inputs provided")
	}

	return inputs, nil
}

func readLinesFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLines(file)
}

func readLines(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
