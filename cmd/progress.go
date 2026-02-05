package cmd

import (
	"fmt"
	"io"
	"sync"
)

// progressBar tracks and renders progress counts to a writer.
type progressBar struct {
	mu       sync.Mutex
	total    int64
	current  int64
	writer   io.Writer
	visible  bool
	finished bool
}

// newProgressCounter returns a progressBar that renders a simple counter.
func newProgressCounter(total int64, writer io.Writer, visible bool) *progressBar {
	return &progressBar{
		total:   total,
		writer:  writer,
		visible: visible,
	}
}

// Add increments the progress counter and renders the updated value.
func (p *progressBar) Add(n int) {
	if p == nil {
		return
	}

	p.mu.Lock()
	p.current += int64(n)
	if p.total > 0 && p.current >= p.total {
		p.finished = true
	}
	visible := p.visible
	writer := p.writer
	total := p.total
	current := p.current
	p.mu.Unlock()

	if !visible {
		return
	}
	if total > 0 {
		fmt.Fprintf(writer, "\r%d/%d", current, total)
		return
	}
	fmt.Fprintf(writer, "\r%d", current)
}

// IsFinished reports whether the counter has reached the total.
func (p *progressBar) IsFinished() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finished
}
