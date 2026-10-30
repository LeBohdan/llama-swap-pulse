// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package llama

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSwap is a minimal llama-swap double: /health, /v1/models and
// /logs/stream/{id} (SSE) against a mutable set of loaded models.
type fakeSwap struct {
	mu     sync.Mutex
	loaded map[string]bool

	streamMu  sync.Mutex
	streamCnt map[string]int
}

func newFakeSwap(loaded map[string]bool) (*httptest.Server, *fakeSwap) {
	f := &fakeSwap{
		loaded:    loaded,
		streamCnt: make(map[string]int),
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler(f, w, r)
	})), f
}

func (f *fakeSwap) setLoaded(loaded map[string]bool) {
	f.mu.Lock()
	f.loaded = loaded
	f.mu.Unlock()
}

func (f *fakeSwap) streamCount(id string) int {
	f.streamMu.Lock()
	defer f.streamMu.Unlock()
	return f.streamCnt[id]
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func streamIDs(m *manager) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.streams))
	for id := range m.streams {
		ids = append(ids, id)
	}
	return ids
}

func TestStreamDiscoveryOpenAndClose(t *testing.T) {
	var fake *fakeSwap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler(fake, w, r)
	}))
	defer srv.Close()

	fake = &fakeSwap{
		loaded:    map[string]bool{"alpha": true},
		streamCnt: make(map[string]int),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := New(srv.URL, 20*time.Millisecond, testLog(), nil).(*manager)

	events := make(chan [2]string, 16)
	m.Subscribe(func(model, line string) { events <- [2]string{model, line} })

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		m.Stop()
		m.Wait()
	}()

	waitUntil(t, 2*time.Second, "stream for alpha", func() bool {
		return len(streamIDs(m)) == 1
	})

	select {
	case ev := <-events:
		if ev[0] != "alpha" {
			t.Fatalf("line tagged with wrong model: %q", ev[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no lines delivered")
	}

	// Model unloads: the stream must be closed.
	fake.setLoaded(map[string]bool{})
	waitUntil(t, 2*time.Second, "stream close", func() bool {
		return len(streamIDs(m)) == 0
	})

	// Model reloads: a fresh stream opens and delivers again.
	fake.setLoaded(map[string]bool{"alpha": true})
	waitUntil(t, 2*time.Second, "stream reopen", func() bool {
		return len(streamIDs(m)) == 1
	})
	select {
	case ev := <-events:
		if ev[0] != "alpha" {
			t.Fatalf("line after reopen tagged with wrong model: %q", ev[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no lines after reopen")
	}
}

// proxyHandler is the shared fake-swap routing (see newFakeSwap).
func proxyHandler(f *fakeSwap, w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/v1/models":
		f.mu.Lock()
		data := make([]string, 0, len(f.loaded))
		for id := range f.loaded {
			data = append(data, fmt.Sprintf(`{"id":%q,"meta":{"llamaswap":{"type":"model"}},"status":{"value":"loaded"}}`, id))
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(data, ","))
	case strings.HasPrefix(r.URL.Path, "/logs/stream/"):
		id := r.URL.Path[len("/logs/stream/"):]
		f.streamMu.Lock()
		f.streamCnt[id]++
		f.streamMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprintf(w, "0.00.100 W srv  stop: cancel task, id_task = %d\n", len(id))
		flusher.Flush()
		<-r.Context().Done()
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestCoveredModelsAreSkipped(t *testing.T) {
	srv, fake := newFakeSwap(map[string]bool{"strata-model": true})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var coveredMu sync.Mutex
	coveredSet := map[string]bool{"strata-model": true}
	m := New(srv.URL, 20*time.Millisecond, testLog(), func() map[string]bool {
		coveredMu.Lock()
		defer coveredMu.Unlock()
		out := make(map[string]bool, len(coveredSet))
		for k, v := range coveredSet {
			out[k] = v
		}
		return out
	}).(*manager)

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		m.Stop()
		m.Wait()
	}()

	// Give discovery a few ticks; the covered model must never get a stream.
	time.Sleep(150 * time.Millisecond)
	if n := len(streamIDs(m)); n != 0 {
		t.Fatalf("covered model got a log stream (streams=%d)", n)
	}
	if c := fake.streamCount("strata-model"); c != 0 {
		t.Fatalf("covered model stream was requested %d times", c)
	}

	// The poller stops covering it: a log stream must open on the next tick.
	coveredMu.Lock()
	coveredSet = map[string]bool{}
	coveredMu.Unlock()
	waitUntil(t, 2*time.Second, "stream for newly uncovered model", func() bool {
		return len(streamIDs(m)) == 1
	})
}

func TestConnectedFollowsHealthEndpoint(t *testing.T) {
	var healthyMu sync.Mutex
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyMu.Lock()
		ok := healthy
		healthyMu.Unlock()
		if r.URL.Path == "/health" {
			if ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(srv.URL, time.Hour, testLog(), nil).(*manager)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		m.Stop()
		m.Wait()
	}()

	waitUntil(t, 2*time.Second, "connected=true", func() bool { return m.Connected() })

	healthyMu.Lock()
	healthy = false
	healthyMu.Unlock()
	m.checkHealth()
	if m.Connected() {
		t.Fatalf("Connected() must be false after a failing health check")
	}

	healthyMu.Lock()
	healthy = true
	healthyMu.Unlock()
	m.checkHealth()
	if !m.Connected() {
		t.Fatalf("Connected() must be true after a passing health check")
	}
}
