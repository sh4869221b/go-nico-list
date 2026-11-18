package niconico

import "context"

// WorkerGate admits target or page workers without allocating a worker pool.
// A positive limit stays fixed; zero follows its control's adaptive budget.
type WorkerGate struct {
	control *HTTPControl
	limit   int
	active  int
	changed chan struct{}
}

// NewWorkerGate builds an independent gate sharing this command's HTTP feedback.
// Waiting scheduling work exposes demand without creating extra HTTP workers.
func (c *HTTPControl) NewWorkerGate(limit int) *WorkerGate {
	if c == nil {
		c = &HTTPControl{}
	}
	return &WorkerGate{control: c, limit: max(0, limit)}
}

func (g *WorkerGate) limitLocked() int {
	if g.limit > 0 {
		return g.limit
	}
	if g.control.adaptive == nil {
		return 1
	}
	return g.control.adaptive.limit
}

// Acquire waits for a worker slot or caller cancellation. Waiting scheduler work
// is counted as demand, including when fixed HTTP admission has no HTTP queue.
func (g *WorkerGate) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := g.control
	c.mu.Lock()
	waiting := false
	err := ctx.Err()
	for err == nil {
		if g.active < g.limitLocked() {
			g.active++
			break
		}
		if !waiting {
			c.workerPending++
			waiting = true
		}
		// Allocate notification channels only when a scheduler has to wait.
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		var resized <-chan struct{}
		if g.limit == 0 && c.adaptive != nil {
			if c.workerChanged == nil {
				c.workerChanged = make(chan struct{})
			}
			resized = c.workerChanged
		}
		changed := g.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		case <-resized:
		}
		c.mu.Lock()
		err = ctx.Err()
	}
	if waiting {
		c.workerPending--
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		g.Release()
		return err
	}
	return nil
}

// Release makes a completed worker slot available to waiting scheduling work.
func (g *WorkerGate) Release() {
	c := g.control
	c.mu.Lock()
	g.active--
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
	c.mu.Unlock()
}
