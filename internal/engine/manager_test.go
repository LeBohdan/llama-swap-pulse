// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"llama-swap-pulse/internal/metrics"
	"llama-swap-pulse/internal/models"
)

// fakeSwap emulates the pair the manager must talk to: a llama-swap control
// plane (GET /running only) and a Strata engine reached directly on its own
// port, whose /metrics live.state follows the given sequence (last state
// repeats). All engine traffic must hit eng.srv — any request to the swap
// proxy routes (or an unexpected swap path) fails the test.
type fakeSwap struct {
	srv *httptest.Server // swap control plane
	eng *httptest.Server // engine direct endpoint

	mu          sync.Mutex
	idx         int
	states      []string
	fixt        map[string][]byte
	ready       bool
	failMetrics bool
	healthN     int
}

func newFakeSwap(t *testing.T, states []string) *fakeSwap {
	t.Helper()

	f := &fakeSwap{states: states, fixt: map[string][]byte{}, ready: true}
	for _, st := range states {
		b, err := os.ReadFile("../../testdata/strata-metrics-" + st + ".json")
		if err != nil {
			t.Fatal(err)
		}
		f.fixt[st] = b
	}

	f.eng = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			f.mu.Lock()
			f.healthN++
			f.mu.Unlock()
			io.WriteString(w, `{"status":"ok","loaded":true,"service":"strata"}`)
		case "/metrics":
			f.mu.Lock()
			fail := f.failMetrics
			i := f.idx
			if i < len(f.states) {
				f.idx++
			} else {
				i = len(f.states) - 1
			}
			b := f.fixt[f.states[i]]
			f.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write(b)
		default:
			t.Errorf("unexpected engine request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/running":
			entries := []map[string]any{
				{"model": "Cold-M", "state": "stopped", "cmd": "llama-server --port 9999"},
			}
			if f.isReady() {
				entries = append(entries, map[string]any{
					"model": "Strata-M", "state": "ready",
					"proxy": f.eng.URL,
					"cmd":   "python serve.py\n--port 8114\n--host 0.0.0.0\n",
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"running": entries})
		default:
			t.Errorf("unexpected swap request %s (engine polling must be direct)", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return f
}

func (f *fakeSwap) isReady() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

func (f *fakeSwap) setReady(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = v
}

func (f *fakeSwap) setFailMetrics(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failMetrics = v
}

// waitForStore drains SSE events (which may be dropped for slow subscribers)
// and polls the store snapshot until cond is true, failing on timeout.
func waitForStore(t *testing.T, ch chan *models.MetricEvent, store *metrics.Store, cond func(metrics.Snapshot) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if cond(store.CurrentSnapshot()) {
			return
		}
		select {
		case <-ch:
		case <-time.After(10 * time.Millisecond):
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for store condition")
		default:
		}
	}
}

// isSubsequence checks that got appears inside want in the same order.
func isSubsequence(got, want []string) bool {
	i := 0
	for _, w := range want {
		if i < len(got) && got[i] == w {
			i++
		}
	}
	return i == len(got)
}

func TestPollerStateMachine(t *testing.T) {
	fake := newFakeSwap(t, []string{"reading", "reading", "generating", "generating", "idle"})
	defer fake.srv.Close()
	defer fake.eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := metrics.NewStore(ctx, time.Minute, time.Hour)
	defer store.Stop()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewManager(fake.srv.URL, store, 10*time.Millisecond, 20*time.Millisecond, log)
	mgr.Start(ctx)
	defer mgr.Stop()

	ch := store.Subscribe()
	defer store.Unsubscribe(ch)

	wantTask := []string{
		"task_started",
		"prefill_progress",
		"prefill_progress",
		"generation_progress",
		"generation_progress",
		"generation_complete",
		"task_finished",
	}

	finished := func(snap metrics.Snapshot) bool {
		for _, ts := range snap.Tasks {
			if ts.Upstream == "Strata-M" && ts.Finished {
				return true
			}
		}
		return false
	}

	// The buffered subscriber channel may drop events; the store snapshot
	// is the source of truth, the SSE order is validated as a subsequence.
	var taskTypes []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "upstream_state" {
				taskTypes = append(taskTypes, ev.Type)
			}
		case <-time.After(10 * time.Millisecond):
		}
		snap := store.CurrentSnapshot()
		if finished(snap) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for finished task, events: %v", taskTypes)
		}
	}

	if !isSubsequence(taskTypes, wantTask) {
		t.Fatalf("unexpected event sequence: %v", taskTypes)
	}

	snap := store.CurrentSnapshot()

	// The engine task uses a synthetic slot keyed by upstream name.
	var task *models.TaskState
	for _, ts := range snap.Tasks {
		if ts.Upstream == "Strata-M" {
			task = ts
		}
	}
	if task == nil {
		t.Fatal("no task state for upstream Strata-M")
	}
	if !task.Finished {
		t.Error("expected task finished")
	}
	if task.Slot <= 0 || task.Task != 1 {
		t.Errorf("expected synthetic slot>0 and task 1, got slot=%d task=%d", task.Slot, task.Task)
	}
	if task.PromptTokens != 12067 {
		t.Errorf("expected prompt_tokens 12067, got %d", task.PromptTokens)
	}
	if task.GeneratedTokens != 12 {
		t.Errorf("expected generated 12, got %d", task.GeneratedTokens)
	}
	if task.PromptEvalMs <= 0 {
		t.Errorf("expected prompt_eval_ms > 0, got %f", task.PromptEvalMs)
	}
	if task.PromptTPS != 1451.3 {
		t.Errorf("expected prompt_tps 1451.3, got %f", task.PromptTPS)
	}
	if task.GenerationTPS != 46.3 {
		t.Errorf("expected generation_tps 46.3, got %f", task.GenerationTPS)
	}

	// Upstream state snapshot with hardware telemetry.
	var us *models.UpstreamState
	for _, u := range snap.Upstreams {
		if u.Upstream == "Strata-M" {
			us = u
		}
	}
	if us == nil {
		t.Fatal("no upstream state for Strata-M")
	}
	if us.Engine != models.EngineStrata {
		t.Errorf("expected engine strata, got %s", us.Engine)
	}
	if us.State != models.StateIdle {
		t.Errorf("expected state idle, got %s", us.State)
	}
	if us.Model != "qwen3.8-flash-next-iq3_s" {
		t.Errorf("unexpected engine model %q", us.Model)
	}
	if us.GPU == nil || us.GPU.MemTotal != 25769803776 {
		t.Errorf("unexpected gpu stats: %+v", us.GPU)
	}
	if us.RAM == nil || us.RAM.Total == 0 {
		t.Errorf("unexpected ram stats: %+v", us.RAM)
	}
}

func TestPollerTaskIdsIncrement(t *testing.T) {
	fake := newFakeSwap(t, []string{
		"reading", "generating", "idle", // first request
		"reading", "generating", "idle", // second request
	})
	defer fake.srv.Close()
	defer fake.eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := metrics.NewStore(ctx, time.Minute, time.Hour)
	defer store.Stop()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewManager(fake.srv.URL, store, 10*time.Millisecond, 20*time.Millisecond, log)
	mgr.Start(ctx)
	defer mgr.Stop()

	ch := store.Subscribe()
	defer store.Unsubscribe(ch)

	// Two finished tasks must be present with distinct task ids (1 and 2):
	// consecutive requests must not overwrite each other's state.
	twoDone := func(snap metrics.Snapshot) bool {
		n := 0
		for _, ts := range snap.Tasks {
			if ts.Upstream == "Strata-M" && ts.Finished {
				n++
			}
		}
		return n >= 2
	}
	waitForStore(t, ch, store, twoDone)

	ids := map[int]bool{}
	for _, ts := range store.CurrentSnapshot().Tasks {
		if ts.Upstream == "Strata-M" {
			ids[ts.Task] = true
		}
	}
	if !ids[1] || !ids[2] || len(ids) != 2 {
		t.Fatalf("expected task ids {1,2}, got %v", ids)
	}
}

// The poller must give up (finalize the task, drop the upstream) after
// maxPollFails consecutive direct /metrics failures, without waiting for the
// next discovery tick.
func TestPollerStopsOnMetricFailures(t *testing.T) {
	fake := newFakeSwap(t, []string{"reading", "generating"})
	defer fake.srv.Close()
	defer fake.eng.Close()
	defer fake.eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := metrics.NewStore(ctx, time.Minute, time.Hour)
	defer store.Stop()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewManager(fake.srv.URL, store, 10*time.Millisecond, time.Hour, log) // discovery off: only failure path can stop it
	mgr.Start(ctx)
	defer mgr.Stop()

	ch := store.Subscribe()
	defer store.Unsubscribe(ch)

	started := func(snap metrics.Snapshot) bool {
		for _, ts := range snap.Tasks {
			if ts.Upstream == "Strata-M" {
				return true
			}
		}
		return false
	}
	waitForStore(t, ch, store, started)

	fake.setFailMetrics(true)

	gone := func(snap metrics.Snapshot) bool { return len(snap.Upstreams) == 0 }
	waitForStore(t, ch, store, gone)

	snap := store.CurrentSnapshot()
	var task *models.TaskState
	for _, ts := range snap.Tasks {
		if ts.Upstream == "Strata-M" {
			task = ts
		}
	}
	if task == nil || !task.Finished {
		t.Fatalf("expected finalized task after metrics failures, got %+v", task)
	}
}

func TestPollerStopsWhenUnloaded(t *testing.T) {
	fake := newFakeSwap(t, []string{"reading", "generating"})
	defer fake.srv.Close()
	defer fake.eng.Close()
	defer fake.eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := metrics.NewStore(ctx, time.Minute, time.Hour)
	defer store.Stop()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewManager(fake.srv.URL, store, 10*time.Millisecond, 20*time.Millisecond, log)
	mgr.Start(ctx)
	defer mgr.Stop()

	ch := store.Subscribe()
	defer store.Unsubscribe(ch)

	started := func(snap metrics.Snapshot) bool {
		for _, ts := range snap.Tasks {
			if ts.Upstream == "Strata-M" {
				return true
			}
		}
		return false
	}
	waitForStore(t, ch, store, started)

	// Make llama-swap drop the model from its running list.
	fake.setReady(false)

	// The poller must finalize the task and remove the upstream.
	gone := func(snap metrics.Snapshot) bool { return len(snap.Upstreams) == 0 }
	waitForStore(t, ch, store, gone)

	snap := store.CurrentSnapshot()
	var task *models.TaskState
	for _, ts := range snap.Tasks {
		if ts.Upstream == "Strata-M" {
			task = ts
		}
	}
	if task == nil || !task.Finished {
		t.Fatalf("expected finalized task after unload, got %+v", task)
	}
}
