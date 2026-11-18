package niconico

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// maxPageBodyBytes allows generous metadata for a 100-item API page while
// bounding buffering even when Content-Length is absent or gzip is expanded.
const maxPageBodyBytes = 8 << 20

// Only retain buffers sized for ordinary pages after decoding.
const maxPooledPageBodyBytes = 128 << 10

var errPageBodyTooLarge = errors.New("response body exceeds 8 MiB limit")
var pageBodyBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

type parsedPage struct {
	Items      []videoItem
	Status     int
	TotalCount *int
	NotFound   bool
}

// releaseItems consumes the page's owned item slice after callers finish using it.
func (p *parsedPage) releaseItems() {
	if len(p.Items) == pageSize {
		projectedPageItemPool.put((*[pageSize]videoItem)(p.Items))
	}
	p.Items = nil
}

// parsePageFunc decodes a page without retaining the input bytes on success.
type parsePageFunc func([]byte) (parsedPage, error)

// readPageBody reads at most one byte past the limit, reusing scratch space for typical pages.
func readPageBody(r io.Reader, contentLength int64, buffer *bytes.Buffer) ([]byte, error) {
	// ReadFrom may grow its buffer to make room for an EOF read. Leave room
	// for that read and use ReadAll for large pages to avoid doubling capacity.
	const reusableLimit = maxPooledPageBodyBytes - bytes.MinRead
	if contentLength >= reusableLimit {
		return io.ReadAll(io.LimitReader(r, maxPageBodyBytes+1))
	}
	limited := &io.LimitedReader{R: r, N: reusableLimit}
	n, err := buffer.ReadFrom(limited)
	body := buffer.Bytes()
	if err == nil && n == reusableLimit {
		limited.N = maxPageBodyBytes + 1 - n
		return io.ReadAll(io.MultiReader(bytes.NewReader(body), limited))
	}
	return body, err
}

func fetchPage(
	ctx context.Context,
	url string,
	httpClientTimeout time.Duration,
	retries int,
	limiter *RateLimiter,
	logger *slog.Logger,
	parsePage parsePageFunc,
	control *HTTPControl,
) (_ parsedPage, retErr error) {
	var metrics *HTTPMetrics
	if control != nil {
		metrics = control.metrics
	}
	metrics.addCounter("pages", 1)
	defer func() {
		if retErr != nil {
			metrics.addCounter("page_errors", 1)
		} else {
			metrics.addCounter("page_success", 1)
		}
	}()
	res, err := retriesRequest(ctx, url, httpClientTimeout, retries, limiter, control)
	if err != nil {
		return parsedPage{}, err
	}
	if res == nil {
		return parsedPage{}, nil
	}
	if res.StatusCode == http.StatusNotFound {
		_ = res.Body.Close()
		return parsedPage{NotFound: true}, nil
	}
	var started time.Time
	if metrics != nil {
		started = time.Now()
	}
	buffer := pageBodyBuffers.Get().(*bytes.Buffer)
	body, err := readPageBody(res.Body, res.ContentLength, buffer)
	if len(body) > maxPageBodyBytes {
		err = errPageBodyTooLarge
	}
	if metrics != nil {
		metrics.observe("body_read", time.Since(started))
		metrics.addCounter("body_bytes", int64(len(body)))
		if err != nil {
			metrics.addCounter("body_errors", 1)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				metrics.addCounter("body_cancellations", 1)
			}
		}
	}
	_ = res.Body.Close()
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		return parsedPage{}, err
	}
	if metrics != nil {
		started = time.Now()
	}
	page, err := parsePage(body)
	if metrics != nil {
		metrics.observe("decode", time.Since(started))
		if err != nil {
			metrics.addCounter("decode_errors", 1)
		}
	}
	if err != nil {
		logger.Error("failed to unmarshal response body", "error", err)
		return parsedPage{}, err
	}
	if buffer.Cap() <= maxPooledPageBodyBytes {
		buffer.Reset()
		pageBodyBuffers.Put(buffer)
	}
	if page.Status != http.StatusOK {
		logger.Warn("unexpected meta status", "status", page.Status)
	}
	return page, nil
}
