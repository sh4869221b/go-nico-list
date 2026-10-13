package niconico

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

type adaptiveCollectionResult struct {
	ids []string
	err error
}

func adaptiveCollectionPage(kind, id string, total *int) string {
	count := ""
	if total != nil {
		field := "totalCount"
		if kind == "mylist" {
			field = "totalItemCount"
		}
		count = fmt.Sprintf(`%q:%d,`, field, *total)
	}
	items := "[]"
	if id != "" {
		field := "essential"
		if kind == "mylist" {
			field = "video"
		}
		items = fmt.Sprintf(`[{%q:{"id":%q,"registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]`, field, id)
	}
	data := fmt.Sprintf(`{%s"items":%s}`, count, items)
	if kind == "mylist" {
		data = `{"mylist":` + data + `}`
	}
	return `{"meta":{"status":200},"data":` + data + `}`
}

func fetchAdaptiveCollection(ctx context.Context, kind, baseURL string, control *HTTPControl) ([]string, error) {
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.DiscardHandler)
	if kind == "mylist" {
		return GetMylistVideoList(ctx, "847130", 0, after, before, baseURL, 1, 5*time.Second, nil, 0, logger, control)
	}
	return GetVideoList(ctx, "12345", 0, after, before, baseURL, 1, 5*time.Second, nil, 0, logger, control)
}

func awaitAdaptivePage(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected page request did not start")
	}
}

func awaitAdaptiveCollection(t *testing.T, results <-chan adaptiveCollectionResult) adaptiveCollectionResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("page collection did not finish")
		return adaptiveCollectionResult{}
	}
}

func TestAdaptivePageConcurrencyKnownCountPreservesOrder(t *testing.T) {
	for _, kind := range []string{"user", "mylist"} {
		t.Run(kind, func(t *testing.T) {
			page3Completed := make(chan struct{})
			var once sync.Once
			total := 300
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("page") {
				case "1":
					_, _ = io.WriteString(w, adaptiveCollectionPage(kind, "sm1", &total))
				case "2":
					select {
					case <-page3Completed:
					case <-r.Context().Done():
						return
					}
					_, _ = io.WriteString(w, adaptiveCollectionPage(kind, "sm2", nil))
				case "3":
					_, _ = io.WriteString(w, adaptiveCollectionPage(kind, "sm3", nil))
					once.Do(func() { close(page3Completed) })
				default:
					t.Errorf("request exceeded known total: page %s", r.URL.Query().Get("page"))
					_, _ = io.WriteString(w, adaptiveCollectionPage(kind, "", nil))
				}
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			results := make(chan adaptiveCollectionResult, 1)
			go func() {
				ids, err := fetchAdaptiveCollection(ctx, kind, server.URL, NewAdaptiveConcurrencyControl(0, false, false))
				results <- adaptiveCollectionResult{ids: ids, err: err}
			}()
			awaitAdaptivePage(t, page3Completed)
			got := awaitAdaptiveCollection(t, results)
			if got.err != nil || !slices.Equal(got.ids, []string{"sm1", "sm2", "sm3"}) {
				t.Fatalf("page-order result: ids=%v error=%v", got.ids, got.err)
			}
		})
	}
}

func TestAdaptivePageConcurrencyUnknownCountRemainsSequential(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	active, peakActive := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		requested = append(requested, page)
		active++
		peakActive = max(peakActive, active)
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		w.Header().Set("Content-Type", "application/json")
		id := ""
		switch page {
		case "1", "2":
			id = "sm" + page
		case "3":
		default:
			t.Errorf("speculative page request: %s", page)
		}
		_, _ = io.WriteString(w, adaptiveCollectionPage("user", id, nil))
	}))
	t.Cleanup(server.Close)
	got, err := fetchAdaptiveCollection(context.Background(), "user", server.URL, NewAdaptiveConcurrencyControl(0, false, false))
	if err != nil || !slices.Equal(got, []string{"sm1", "sm2"}) {
		t.Fatalf("unknown-count result: ids=%v error=%v", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if peakActive != 1 || !slices.Equal(requested, []string{"1", "2", "3"}) {
		t.Fatalf("unknown total used parallel or speculative requests: peak=%d pages=%v", peakActive, requested)
	}
}

func TestAdaptivePageConcurrencyResizeStartsWaitingPage(t *testing.T) {
	control := NewAdaptiveConcurrencyControl(0, true, false)
	setAdaptiveWorkerBudget(control, 1)
	page2Started, page3Started := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	total := 300
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "1" {
			_, _ = io.WriteString(w, adaptiveCollectionPage("user", "sm1", &total))
			return
		}
		switch page {
		case "2":
			close(page2Started)
		case "3":
			close(page3Started)
		default:
			t.Errorf("request exceeded known total: page %s", page)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, adaptiveCollectionPage("user", "sm"+page, nil))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(unblock)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := make(chan adaptiveCollectionResult, 1)
	go func() {
		ids, err := fetchAdaptiveCollection(ctx, "user", server.URL, control)
		results <- adaptiveCollectionResult{ids: ids, err: err}
	}()
	awaitAdaptivePage(t, page2Started)
	waitForWorkerPending(t, control, 1)
	if got := control.Snapshot().Admission.Reserved; got != 1 {
		t.Fatalf("worker budget one allowed parallel pages with unlimited HTTP: %d", got)
	}
	setAdaptiveWorkerBudget(control, 2)
	awaitAdaptivePage(t, page3Started)
	unblock()
	got := awaitAdaptiveCollection(t, results)
	if got.err != nil || !slices.Equal(got.ids, []string{"sm1", "sm2", "sm3"}) {
		t.Fatalf("resized page result: ids=%v error=%v", got.ids, got.err)
	}
}

func TestAdaptivePageConcurrencyIgnoresErrorsAfterNaturalEnd(t *testing.T) {
	for _, terminalStatus := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(strconv.Itoa(terminalStatus), func(t *testing.T) {
			page3Started := make(chan struct{})
			total := 300
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("page") {
				case "1":
					_, _ = io.WriteString(w, adaptiveCollectionPage("user", "sm1", &total))
				case "2":
					select {
					case <-page3Started:
					case <-r.Context().Done():
						return
					}
					w.WriteHeader(terminalStatus)
					if terminalStatus == http.StatusOK {
						_, _ = io.WriteString(w, adaptiveCollectionPage("user", "", nil))
					}
				case "3":
					close(page3Started)
					w.WriteHeader(http.StatusInternalServerError)
				default:
					t.Errorf("request exceeded known total: page %s", r.URL.Query().Get("page"))
				}
			}))
			t.Cleanup(server.Close)
			got, err := fetchAdaptiveCollection(context.Background(), "user", server.URL, NewAdaptiveConcurrencyControl(0, false, false))
			if err != nil || !slices.Equal(got, []string{"sm1"}) {
				t.Fatalf("result after natural end: ids=%v error=%v", got, err)
			}
		})
	}
}

func TestAdaptivePageConcurrencyCancellationDrainsWaitingScheduler(t *testing.T) {
	control := NewAdaptiveConcurrencyControl(0, true, false)
	setAdaptiveWorkerBudget(control, 2)
	page2Started, page3Started := make(chan struct{}), make(chan struct{})
	total := 1000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, adaptiveCollectionPage("user", "sm1", &total))
			return
		case "2":
			close(page2Started)
		case "3":
			close(page3Started)
		default:
			t.Errorf("request escaped worker gate: page %s", r.URL.Query().Get("page"))
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := make(chan adaptiveCollectionResult, 1)
	go func() {
		ids, err := fetchAdaptiveCollection(ctx, "user", server.URL, control)
		results <- adaptiveCollectionResult{ids: ids, err: err}
	}()
	awaitAdaptivePage(t, page2Started)
	awaitAdaptivePage(t, page3Started)
	waitForWorkerPending(t, control, 1)
	cancel()
	got := awaitAdaptiveCollection(t, results)
	if got.err != nil || len(got.ids) != 0 {
		t.Fatalf("cancellation result: ids=%v error=%v", got.ids, got.err)
	}
	waitForWorkerPending(t, control, 0)
	if got := control.Snapshot().Admission; got.Reserved != 0 || got.Pending != 0 {
		t.Fatalf("cancellation leaked HTTP admission: %+v", got)
	}
}

func TestAdaptivePageConcurrencyMaximumTotalStopsAtEmptyPage(t *testing.T) {
	control := NewAdaptiveConcurrencyControl(0, true, false)
	setAdaptiveWorkerBudget(control, 1)
	total := int(^uint(0) >> 1)
	var requested []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		requested = append(requested, page)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = io.WriteString(w, adaptiveCollectionPage("user", "sm1", &total))
		case "2":
			_, _ = io.WriteString(w, adaptiveCollectionPage("user", "", nil))
		default:
			t.Errorf("request after empty page: %s", page)
			_, _ = io.WriteString(w, adaptiveCollectionPage("user", "", nil))
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	got, err := fetchAdaptiveCollection(ctx, "user", server.URL, control)
	if err != nil || !slices.Equal(got, []string{"sm1"}) {
		t.Fatalf("large-total result: ids=%v error=%v", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(requested, []string{"1", "2"}) {
		t.Fatalf("large total did not fetch page two and stop: %v", requested)
	}
}
