package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRunRootCmdPageConcurrency(t *testing.T) {
	for _, targetType := range []string{targetTypeUser, targetTypeMylist} {
		t.Run(targetType, func(t *testing.T) {
			page3Started := make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var ids []string
				switch r.URL.Query().Get("page") {
				case "1":
					ids = []string{"sm3"}
				case "2":
					select {
					case <-page3Started:
						ids = []string{"sm2"}
					case <-r.Context().Done():
						return
					}
				case "3":
					once.Do(func() { close(page3Started) })
					ids = []string{"sm1"}
				}
				_, _ = io.WriteString(w, httpCommandPagePayload(targetType == targetTypeMylist, 300, ids))
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() { once.Do(func() { close(page3Started) }) })
			cfg := testFetchConfig(server.URL)
			cfg.PageConcurrency = 2
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)
			cmd, out, _ := newTestRootCommand(t, cfg, newTestRootDeps())
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{fmt.Sprintf("nicovideo.jp/%s/1", targetType)})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := out.String(); got != "sm1\nsm2\nsm3\n" {
				t.Fatalf("unexpected stdout output: %q", got)
			}
		})
	}
}
