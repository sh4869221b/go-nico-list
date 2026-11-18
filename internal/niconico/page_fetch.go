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
// The returned byte count includes bytes received before a read error.
func readPageBody(r io.Reader, buffer *bytes.Buffer) ([]byte, int, error) {
	// ReadFrom may grow its buffer to make room for an EOF read. Leave room
	// for that read so the reusable buffer stays within its capacity limit.
	const reusableLimit = maxPooledPageBodyBytes - bytes.MinRead
	_, err := buffer.ReadFrom(io.LimitReader(r, reusableLimit))
	body := buffer.Bytes()
	if err == nil && len(body) == reusableLimit {
		return readLargePageBody(r, body)
	}
	return body, len(body), err
}

// readLargePageBody assembles the prefix and subsequent chunks only after a successful final read.
func readLargePageBody(r io.Reader, prefix []byte) ([]byte, int, error) {
	// Keep chunks separate until EOF so rejected bodies need no contiguous copy.
	const maxChunkSize = 512 << 10
	chunks := make([][]byte, 1, 8)
	chunks[0] = prefix
	total := len(prefix)
	chunk := make([]byte, 0, 16<<10)
	chunkSize := 64 << 10
	for {
		if len(chunk) == cap(chunk) {
			chunks = append(chunks, chunk)
			chunk = make([]byte, 0, min(chunkSize, maxPageBodyBytes+1-total))
			chunkSize = min(2*chunkSize, maxChunkSize)
		}
		n, err := r.Read(chunk[len(chunk):cap(chunk)])
		chunk = chunk[:len(chunk)+n]
		total += n
		if total > maxPageBodyBytes {
			return nil, total, errPageBodyTooLarge
		}
		if err == io.EOF {
			if total == len(prefix) {
				return prefix, total, nil
			}
			return bytes.Join(append(chunks, chunk), nil), total, nil
		}
		if err != nil {
			return nil, total, err
		}
	}
}

// putPageBodyBuffer resets reusable scratch space after the caller releases its contents.
func putPageBodyBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() <= maxPooledPageBodyBytes {
		buffer.Reset()
		pageBodyBuffers.Put(buffer)
	}
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
	body, bodyBytes, err := readPageBody(res.Body, buffer)
	if metrics != nil {
		metrics.observe("body_read", time.Since(started))
		metrics.addCounter("body_bytes", int64(bodyBytes))
		if err != nil {
			metrics.addCounter("body_errors", 1)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				metrics.addCounter("body_cancellations", 1)
			}
		}
	}
	_ = res.Body.Close()
	if err != nil {
		putPageBodyBuffer(buffer)
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
	putPageBodyBuffer(buffer)
	if page.Status != http.StatusOK {
		logger.Warn("unexpected meta status", "status", page.Status)
	}
	return page, nil
}
