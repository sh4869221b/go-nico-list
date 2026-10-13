package niconico

import (
	"container/list"
	"context"
	"sync"
)

// HTTPControl owns admission and optional measurements for one command execution.
// A nil control preserves the original unlimited, uninstrumented request path.
type HTTPControl struct {
	mu            sync.Mutex
	hardMax       int
	limit         int
	reserved      int
	peakReserved  int
	peakPending   int
	queue         list.List
	metrics       *HTTPMetrics
	adaptive      *adaptiveController
	inFlight      int
	pauseEpoch    uint64
	httpFixed     bool
	workerPending int
	workerChanged chan struct{}
}

type httpWaiter struct {
	ready   chan struct{}
	granted bool
}

// HTTPAdmissionSnapshot distinguishes reserved slots (including rate waiting)
// from requests actually executing in HTTPMetricsSnapshot.
type HTTPAdmissionSnapshot struct {
	HardMax      int `json:"hard_max"`
	Limit        int `json:"limit"`
	Reserved     int `json:"reserved"`
	PeakReserved int `json:"peak_reserved"`
	Pending      int `json:"pending"`
	PeakPending  int `json:"peak_pending"`
}

// HTTPControlSnapshot is a bounded, content-free command diagnostic.
type HTTPControlSnapshot struct {
	Admission HTTPAdmissionSnapshot `json:"admission"`
	Metrics   HTTPMetricsSnapshot   `json:"metrics"`
	Adaptive  *AdaptiveSnapshot     `json:"adaptive,omitempty"`
}

// NewHTTPControl builds command-wide admission. Callers validate nonnegative limits.
func NewHTTPControl(limit int, metrics bool) *HTTPControl {
	if limit <= 0 && !metrics {
		return nil
	}
	c := &HTTPControl{hardMax: limit, limit: limit}
	if metrics {
		c.metrics = NewHTTPMetrics()
	}
	return c
}

// NewAdaptiveConcurrencyControl shares HTTP feedback with target/page admission.
// A fixed HTTP limit is preserved exactly, including zero for unlimited HTTP.
func NewAdaptiveConcurrencyControl(limit int, httpFixed bool, metrics bool) *HTTPControl {
	if limit < 0 {
		return nil
	}
	c := &HTTPControl{hardMax: limit, httpFixed: httpFixed, workerChanged: make(chan struct{})}
	if metrics {
		c.metrics = NewHTTPMetrics()
	}
	c.adaptive = newAdaptiveController(limit, timeNow())
	if httpFixed {
		c.limit = limit
	} else {
		c.limit = c.adaptive.limit
	}
	return c
}

// Snapshot returns an independent copy suitable for structured logging.
func (c *HTTPControl) Snapshot() HTTPControlSnapshot {
	if c == nil {
		return HTTPControlSnapshot{}
	}
	c.mu.Lock()
	admission := HTTPAdmissionSnapshot{HardMax: c.hardMax, Limit: c.limit, Reserved: c.reserved, PeakReserved: c.peakReserved, Pending: c.queue.Len(), PeakPending: c.peakPending}
	var adaptive *AdaptiveSnapshot
	if c.adaptive != nil {
		state := c.adaptive.snapshot(timeNow())
		state.Pending, state.InFlight = c.pendingLocked(), c.inFlight
		adaptive = &state
	}
	c.mu.Unlock()
	return HTTPControlSnapshot{Admission: admission, Metrics: c.metrics.Snapshot(), Adaptive: adaptive}
}

func (c *HTTPControl) pendingLocked() int {
	return c.queue.Len() + min(c.workerPending, int(^uint(0)>>1)-c.queue.Len())
}

// updateAdaptiveLimitLocked publishes a resize to both HTTP and worker admission.
// Existing workers and requests drain normally after a decrease.
func (c *HTTPControl) updateAdaptiveLimitLocked(previousLimit int) {
	if !c.httpFixed {
		c.limit = c.adaptive.limit
	}
	if previousLimit != c.adaptive.limit && c.workerChanged != nil {
		close(c.workerChanged)
		c.workerChanged = make(chan struct{})
	}
	c.grantLocked()
}

func (c *HTTPControl) acquire(ctx context.Context) error {
	if c == nil {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.queue.Len() == 0 && (c.limit == 0 || c.reserved < c.limit) {
		c.reserveLocked()
		c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			c.release()
			return err
		}
		return nil
	}
	waiter := &httpWaiter{ready: make(chan struct{})}
	element := c.queue.PushBack(waiter)
	c.peakPending = max(c.peakPending, c.queue.Len())
	c.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-waiter.ready:
	}
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		if waiter.granted {
			c.reserved--
		} else {
			c.queue.Remove(element)
		}
		c.grantLocked()
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	return nil
}

func (c *HTTPControl) reserveLocked() {
	c.reserved++
	c.peakReserved = max(c.peakReserved, c.reserved)
}

func (c *HTTPControl) grantLocked() {
	for c.queue.Len() > 0 && (c.limit == 0 || c.reserved < c.limit) {
		element := c.queue.Front()
		waiter := element.Value.(*httpWaiter)
		c.queue.Remove(element)
		c.reserveLocked()
		waiter.granted = true
		close(waiter.ready)
	}
}

func (c *HTTPControl) release() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.reserved--
	c.grantLocked()
	c.mu.Unlock()
}
