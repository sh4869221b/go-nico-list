package cmd

import (
	"io"
	"testing"

	"github.com/schollz/progressbar/v3"
)

func TestRunRootCmdInvalidInput(t *testing.T) {
	var bar *progressbar.ProgressBar
	deps := newTestRootDeps()
	deps.IsTerminal = func(io.Writer) bool { return true }
	deps.ProgressBarNew = func(max int64, writer io.Writer, visible bool) *progressbar.ProgressBar {
		bar = progressbar.NewOptions64(max, progressbar.OptionSetWriter(writer), progressbar.OptionSetVisibility(visible))
		return bar
	}
	cfg := newTestRootConfig()
	cfg.NoProgress = false

	_, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bar == nil || !bar.IsFinished() {
		t.Errorf("progress bar not finished")
	}
}

func TestProgressAutoDisabledOnNonTTY(t *testing.T) {
	var visible bool
	deps := newTestRootDeps()
	deps.IsTerminal = func(io.Writer) bool { return false }
	deps.ProgressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show))
	}
	cfg := newTestRootConfig()
	cfg.NoProgress = false
	cfg.ForceProgress = false

	_, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if visible {
		t.Errorf("expected progress to be hidden on non-TTY stderr")
	}
}

func TestProgressForcedOn(t *testing.T) {
	var visible bool
	deps := newTestRootDeps()
	deps.IsTerminal = func(io.Writer) bool { return false }
	deps.ProgressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show))
	}
	cfg := newTestRootConfig()
	cfg.ForceProgress = true
	cfg.NoProgress = false

	_, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !visible {
		t.Errorf("expected progress to be visible when forced")
	}
}

func TestNoProgressOverridesForceProgress(t *testing.T) {
	var visible bool
	deps := newTestRootDeps()
	deps.IsTerminal = func(io.Writer) bool { return true }
	deps.ProgressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
		visible = show
		return progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show))
	}
	cfg := newTestRootConfig()
	cfg.ForceProgress = true
	cfg.NoProgress = true

	_, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if visible {
		t.Errorf("expected progress to be hidden when no-progress is set")
	}
}
