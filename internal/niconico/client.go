package niconico

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"
)

const (
	tabStr = "\t\t\t\t\t\t\t\t\t"
	urlStr = "https://www.nicovideo.jp/watch/"
)

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

// GetVideoList retrieves video IDs for a user.
func GetVideoList(
	ctx context.Context,
	userID string,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	tab bool,
	url bool,
	baseURL string,
	retries int,
	httpClientTimeout time.Duration,
	logger *slog.Logger,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var resStr []string

	var beforeStr = ""
	if tab {
		beforeStr += tabStr
	}
	if url {
		beforeStr += urlStr
	}

	for page := 1; ; page++ {
		requestURL := fmt.Sprintf("%s/users/%s/videos?pageSize=100&page=%d", baseURL, userID, page)
		res, err := retriesRequest(ctx, requestURL, httpClientTimeout, retries)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}
			return resStr, err
		}
		if res != nil {
			if res.StatusCode == http.StatusNotFound {
				break
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				logger.Error("failed to read response body", "error", err)
				return resStr, err
			}

			var nicoData NicoData
			if err := json.Unmarshal(body, &nicoData); err != nil {
				logger.Error("failed to unmarshal response body", "error", err)
				return resStr, err
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
				if !s.Essential.RegisteredAt.Before(beforeDate.AddDate(0, 0, 1)) {
					continue
				}
				resStr = append(resStr, fmt.Sprintf("%s%s", beforeStr, s.Essential.ID))
			}
		}
	}
	return resStr, nil
}

func retriesRequest(ctx context.Context, url string, httpClientTimeout time.Duration, retries int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: httpClientTimeout}

	var (
		res *http.Response
	)
	const baseDelay = 100 * time.Millisecond
	maxRetries := retries
	attempts := retries

	for attempts > 0 {
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
		attempts--
		wait := time.Duration(math.Min(math.Pow(2, float64(maxRetries-attempts))*float64(baseDelay), float64(30*time.Second)))
		time.Sleep(wait)
	}

	if err != nil {
		return nil, err
	}

	return res, nil
}
