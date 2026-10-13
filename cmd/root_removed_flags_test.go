package cmd

import (
	"strings"
	"testing"
)

func TestRemovedFlagsReturnUnknownFlag(t *testing.T) {
	tests := []struct {
		arg     string
		wantErr string
	}{
		{arg: "-t", wantErr: "unknown shorthand flag: 't' in -t"},
		{arg: "-n", wantErr: "unknown shorthand flag: 'n' in -n"},
		{arg: "--tab", wantErr: "unknown flag: --tab"},
		{arg: "--max-pages=1", wantErr: "unknown flag: --max-pages"},
		{arg: "--max-videos=1", wantErr: "unknown flag: --max-videos"},
		{arg: "--concurrency=3", wantErr: "unknown flag: --concurrency"},
		{arg: "--page-concurrency=1", wantErr: "unknown flag: --page-concurrency"},
		{arg: "--http-concurrency=0", wantErr: "unknown flag: --http-concurrency"},
		{arg: "--adaptive-concurrency", wantErr: "unknown flag: --adaptive-concurrency"},
		{arg: "--adaptive-http-concurrency", wantErr: "unknown flag: --adaptive-http-concurrency"},
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
