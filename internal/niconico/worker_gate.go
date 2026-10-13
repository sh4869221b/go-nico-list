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
	return &WorkerGate{control: c, limit: max(0, limit), changed: make(chan struct{})}
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
	for {
		if err := ctx.Err(); err != nil {
			if waiting {
				c.workerPending--
			}
			c.mu.Unlock()
			return err
		}
		if g.active < g.limitLocked() {
			g.active++
			if waiting {
				c.workerPending--
			}
			c.mu.Unlock()
			if err := ctx.Err(); err != nil {
				g.Release()
				return err
			}
			return nil
		}
		if !waiting {
			c.workerPending++
			waiting = true
		}
		changed, resized := g.changed, c.workerChanged
		c.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		case <-resized:
		}
		c.mu.Lock()
	}
}

// Release makes a completed worker slot available to waiting scheduling work.
func (g *WorkerGate) Release() {
	c := g.control
	c.mu.Lock()
	g.active--
	close(g.changed)
	g.changed = make(chan struct{})
	c.mu.Unlock()
}
