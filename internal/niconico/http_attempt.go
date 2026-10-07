package niconico

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

// waitForHTTPAttempt keeps retry sleeping outside admission. Without a cap,
// preserve the original combined rate/backoff reservation behavior exactly.
func waitForHTTPAttempt(ctx context.Context, limiter *RateLimiter, delay time.Duration, control *HTTPControl) error {
	if control == nil {
		return limiter.Wait(ctx, delay)
	}
	metrics := control.metrics
	if control.hardMax == 0 {
		wait := delay
		if limiter != nil {
			wait = limiter.reserveDelay(delay)
		}
		started := time.Now()
		err := sleepFn(ctx, wait)
		elapsed := time.Since(started)
		if delay > 0 {
			metrics.observe("backoff_wait", min(elapsed, delay))
		}
		if limiter != nil {
			metrics.observe("rate_wait", max(0, elapsed-min(elapsed, delay)))
		}
		if err != nil {
			metrics.addCounter("wait_cancellations", 1)
			return err
		}
	} else if delay > 0 {
		started := time.Now()
		err := sleepFn(ctx, delay)
		metrics.observe("backoff_wait", time.Since(started))
		if err != nil {
			metrics.addCounter("wait_cancellations", 1)
			return err
		}
	}
	started := time.Now()
	err := control.acquire(ctx)
	metrics.observe("semaphore_wait", time.Since(started))
	if err != nil {
		metrics.addCounter("wait_cancellations", 1)
		return err
	}
	if control.hardMax > 0 && limiter != nil {
		started = time.Now()
		err = limiter.Wait(ctx, 0)
		metrics.observe("rate_wait", time.Since(started))
		if err != nil {
			control.release()
			metrics.addCounter("wait_cancellations", 1)
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		control.release()
		metrics.addCounter("wait_cancellations", 1)
		return err
	}
	return nil
}

func performHTTPAttempt(client *http.Client, req *http.Request, attempt int, control *HTTPControl, adaptive *adaptiveAttempt) (*http.Response, error) {
	if control == nil {
		return client.Do(req)
	}
	metrics := control.metrics
	var started time.Time
	var stopTrace func()
	if metrics != nil {
		started = time.Now()
		trace, stop := metrics.trace(started)
		stopTrace = stop
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		metrics.startAttempt(attempt > 1)
	}
	res, err := client.Do(req)
	if err != nil {
		status := 0
		if res != nil {
			status = res.StatusCode
			if res.Body != nil {
				closeStarted := time.Now()
				closeErr := res.Body.Close()
				if metrics != nil {
					metrics.observe("body_close", time.Since(closeStarted))
					if closeErr != nil {
						metrics.addCounter("close_errors", 1)
					}
				}
			}
		}
		if stopTrace != nil {
			stopTrace()
		}
		if metrics != nil {
			metrics.endAttempt(status, err, time.Since(started))
		}
		control.finishAdaptive(adaptive, req.Context(), status, err, nil, false, nil)
		return nil, err
	}
	if adaptive != nil && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		delay := nextRetryDelay(retryAfterDelay(res), attempt)
		adaptive.retryAt = timeNow().Add(delay)
		if res.StatusCode == http.StatusTooManyRequests {
			control.throttleAdaptive(adaptive, delay)
		}
	}
	res.Body = &controlledBody{ReadCloser: res.Body, control: control, started: started, status: res.StatusCode, stopTrace: stopTrace, adaptive: adaptive, ctx: req.Context()}
	return res, nil
}

type controlledBody struct {
	io.ReadCloser
	control   *HTTPControl
	started   time.Time
	status    int
	stopTrace func()
	once      sync.Once
	closeErr  error
	adaptive  *adaptiveAttempt
	ctx       context.Context
	readMu    sync.Mutex
	readErr   error
	eof       bool
}

func (b *controlledBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.adaptive != nil && err != nil {
		b.readMu.Lock()
		if err == io.EOF {
			b.eof = true
		} else {
			b.readErr = err
		}
		b.readMu.Unlock()
	}
	return n, err
}

func (b *controlledBody) Close() error {
	b.once.Do(func() {
		metrics := b.control.metrics
		var started time.Time
		if metrics != nil {
			started = time.Now()
		}
		b.closeErr = b.ReadCloser.Close()
		if b.stopTrace != nil {
			b.stopTrace()
		}
		if metrics != nil {
			metrics.observe("body_close", time.Since(started))
			if b.closeErr != nil {
				metrics.addCounter("close_errors", 1)
			}
			metrics.endAttempt(b.status, nil, time.Since(b.started))
		}
		b.readMu.Lock()
		readErr, eof := b.readErr, b.eof
		b.readMu.Unlock()
		b.control.finishAdaptive(b.adaptive, b.ctx, b.status, nil, readErr, eof, b.closeErr)
	})
	return b.closeErr
}
