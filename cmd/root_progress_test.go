package cmd

import (
	"io"
	"testing"

	"github.com/schollz/progressbar/v3"
)

func TestRunRootCmdProgress(t *testing.T) {
	for _, test := range []struct {
		name                        string
		terminal, force, hide, show bool
	}{
		{name: "terminal", terminal: true, show: true},
		{name: "non_terminal"},
		{name: "forced", force: true, show: true},
		{name: "hidden_overrides_forced", terminal: true, force: true, hide: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := newTestRootConfig()
			cfg.NoProgress, cfg.ForceProgress = test.hide, test.force
			deps := newTestRootDeps()
			deps.IsTerminal = func(io.Writer) bool { return test.terminal }
			var bar *progressbar.ProgressBar
			var visible bool
			deps.ProgressBarNew = func(max int64, writer io.Writer, show bool) *progressbar.ProgressBar {
				visible = show
				bar = progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show))
				return bar
			}
			out, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Len() != 0 {
				t.Fatalf("invalid input wrote stdout: %q", out.String())
			}
			if bar == nil || visible != test.show || (visible && !bar.IsFinished()) {
				t.Fatalf("progress finished=%t visible=%t, want visible=%t", bar != nil && bar.IsFinished(), visible, test.show)
			}
		})
	}
}
