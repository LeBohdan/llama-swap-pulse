// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package metrics

import (
	"context"
	"testing"
	"time"

	"llama-swap-pulse/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(context.Background(), time.Minute, 10*time.Minute)
	t.Cleanup(s.Stop)
	return s
}

// Two llama.cpp processes using the same slot/task ids must not collide:
// with per-model streams both tasks carry Upstream and the keys differ.
func TestTwoModelsSameSlotTaskDoNotCollide(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	for _, up := range []string{"model-a", "model-b"} {
		if !s.Update(&models.MetricEvent{
			Type: "task_started", Upstream: up, Slot: 0, Task: 1, Timestamp: now,
		}) {
			t.Fatalf("task_started for %s not emitted", up)
		}
	}

	snap := s.CurrentSnapshot()
	if len(snap.Tasks) != 2 {
		t.Fatalf("expected 2 distinct tasks, got %d: %+v", len(snap.Tasks), snap.Tasks)
	}
	seen := map[string]bool{}
	for _, ts := range snap.Tasks {
		if ts.Upstream == "" || ts.Slot != 0 || ts.Task != 1 {
			t.Fatalf("unexpected task state: %+v", ts)
		}
		seen[ts.Upstream] = true
	}
	if !seen["model-a"] || !seen["model-b"] {
		t.Fatalf("expected both upstreams, got %v", seen)
	}
}

func TestTaskCancelledScopesToUpstream(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	for _, up := range []string{"model-a", "model-b"} {
		s.Update(&models.MetricEvent{
			Type: "task_started", Upstream: up, Slot: 0, Task: 7, Timestamp: now,
		})
	}

	// Cancel only the model-a task (its per-model stream supplies Upstream).
	s.Update(&models.MetricEvent{
		Type: "task_cancelled", Upstream: "model-a", Task: 7, Timestamp: now,
	})

	snap := s.CurrentSnapshot()
	if len(snap.Tasks) != 1 {
		t.Fatalf("expected 1 remaining task, got %d", len(snap.Tasks))
	}
	if snap.Tasks[0].Upstream != "model-b" {
		t.Fatalf("expected model-b task to remain, got %q", snap.Tasks[0].Upstream)
	}
}

func TestTaskCancelledFallsBackToTaskID(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	s.Update(&models.MetricEvent{
		Type: "task_started", Upstream: "model-a", Slot: 0, Task: 9, Timestamp: now,
	})

	// A cancelled line whose Upstream matches no known task still resolves
	// by task id alone (stray/delayed line safety net).
	s.Update(&models.MetricEvent{
		Type: "task_cancelled", Upstream: "model-b", Task: 9, Timestamp: now,
	})

	if snap := s.CurrentSnapshot(); len(snap.Tasks) != 0 {
		t.Fatalf("expected the task to be removed, got %d tasks", len(snap.Tasks))
	}
}
