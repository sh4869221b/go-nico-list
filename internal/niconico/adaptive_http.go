package niconico

import (
	"context"
	"net/http"
	"time"
)

// adaptiveAttempt is a single dispatched attempt, independent of optional tracing.
type adaptiveAttempt struct {
	generation uint64
	started    time.Time
	rateWait   time.Duration
	retryAt    time.Time
}

// adaptiveHTTPAttempt combines independent deadlines without holding a slot
// during retry, shared cooldown, or recovery pacing. A rate grant acquired before
// a new 429 is discarded rather than carried across that pause.
func adaptiveHTTPAttempt(client *http.Client, req *http.Request, attempt int, retryAt time.Time, limiter *RateLimiter, control *HTTPControl) (*http.Response, error) {
	ctx := req.Context()
	var rateWait time.Duration
	for {
		if err := control.waitAdaptiveReady(ctx, retryAt); err != nil {
			return nil, err
		}
		started := timeNow()
		err := control.acquire(ctx)
		control.metrics.observe("semaphore_wait", timeNow().Sub(started))
		if err != nil {
			control.metrics.addCounter("wait_cancellations", 1)
			return nil, err
		}
		pauseEpoch := control.adaptivePauseEpoch()
		if limiter != nil {
			started = timeNow()
			err = limiter.Wait(ctx, 0)
			waited := timeNow().Sub(started)
			rateWait += waited
			control.metrics.observe("rate_wait", waited)
			if err != nil {
				control.release()
				control.metrics.addCounter("wait_cancellations", 1)
				return nil, err
			}
		}
		permit, ready, err := control.adaptiveDispatch(ctx, rateWait, pauseEpoch)
		if err != nil {
			control.release()
			control.metrics.addCounter("wait_cancellations", 1)
			return nil, err
		}
		if !ready {
			control.release()
			continue
		}
		return performHTTPAttempt(client, req, attempt > 1, control, &permit, attempt)
	}
}

func (c *HTTPControl) waitAdaptiveReady(ctx context.Context, retryAt time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			c.metrics.addCounter("wait_cancellations", 1)
			return err
		}
		now := timeNow()
		c.mu.Lock()
		ready := c.adaptive.readyAt()
		c.mu.Unlock()
		if retryAt.After(ready) {
			ready = retryAt
		}
		delay := ready.Sub(now)
		if delay <= 0 {
			return nil
		}
		err := sleepFn(ctx, delay)
		elapsed := max(0, timeNow().Sub(now))
		backoff := min(elapsed, max(0, retryAt.Sub(now)))
		if backoff > 0 {
			c.metrics.observe("backoff_wait", backoff)
		}
		if elapsed > backoff {
			c.metrics.observe("adaptive_wait", elapsed-backoff)
		}
		if err != nil {
			c.metrics.addCounter("wait_cancellations", 1)
			return err
		}
	}
}

func (c *HTTPControl) adaptivePauseEpoch() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pauseEpoch
}

func (c *HTTPControl) adaptiveDispatch(ctx context.Context, rateWait time.Duration, pauseEpoch uint64) (adaptiveAttempt, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return adaptiveAttempt{}, false, err
	}
	now := timeNow()
	if pauseEpoch != c.pauseEpoch || c.adaptive.readyAt().After(now) || c.inFlight >= c.limit {
		return adaptiveAttempt{}, false, nil
	}
	c.adaptive.dispatched(now)
	c.inFlight++
	return adaptiveAttempt{generation: c.adaptive.generation, started: now, rateWait: rateWait}, true, nil
}

func (c *HTTPControl) throttleAdaptive(attempt *adaptiveAttempt, delay time.Duration) {
	c.mu.Lock()
	previous := c.adaptive.cooldownUntil
	c.adaptive.throttle(timeNow(), attempt.generation, delay)
	if c.adaptive.cooldownUntil.After(previous) {
		c.pauseEpoch++
	}
	c.limit = c.adaptive.limit
	c.grantLocked()
	c.mu.Unlock()
}

// finishAdaptive observes complete bodies, including failures hidden by a 200
// header. Caller cancellation stays neutral; a client timeout with a live caller
// is an overload signal. Parsing occurs later and cannot inflate service latency.
func (c *HTTPControl) finishAdaptive(attempt *adaptiveAttempt, ctx context.Context, status int, transportErr, readErr error, eof bool, closeErr error) {
	if attempt == nil {
		c.release()
		return
	}
	sample := adaptiveSample{Generation: attempt.generation, Service: max(0, timeNow().Sub(attempt.started)), RateWait: attempt.rateWait}
	if ctx.Err() == nil {
		sample.Success = status == http.StatusOK && transportErr == nil && readErr == nil && eof && closeErr == nil
		sample.Overload = (status >= 500 && status < 600) || transportErr != nil || readErr != nil
	}
	c.mu.Lock()
	c.inFlight--
	c.adaptive.observe(timeNow(), sample, adaptiveLoad{Pending: c.queue.Len(), InFlight: c.inFlight})
	c.limit = c.adaptive.limit
	c.reserved--
	c.grantLocked()
	c.mu.Unlock()
}
