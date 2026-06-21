package cmd

import (
	"strings"
	"testing"
)

func TestRemovedFlagsAreNotRegistered(t *testing.T) {
	cmd := NewRootCommand(newTestRootConfig(), newTestRootDeps())

	for _, name := range []string{"tab", "max-pages", "max-videos"} {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			t.Errorf("flag %q is still registered", name)
		}
	}
	if flag := cmd.Flags().ShorthandLookup("t"); flag != nil {
		t.Errorf("shorthand flag %q is still registered", "t")
	}
}

func TestRemovedFlagsReturnUnknownFlag(t *testing.T) {
	tests := []struct {
		arg     string
		wantErr string
	}{
		{arg: "-t", wantErr: "unknown shorthand flag: 't' in -t"},
		{arg: "--tab", wantErr: "unknown flag: --tab"},
		{arg: "--max-pages=1", wantErr: "unknown flag: --max-pages"},
		{arg: "--max-videos=1", wantErr: "unknown flag: --max-videos"},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			stdout, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), tt.arg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}
