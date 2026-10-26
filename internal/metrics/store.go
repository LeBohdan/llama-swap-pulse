// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package metrics

import (
	"context"
	"sort"
	"sync"
	"time"

	"llama-swap-pulse/internal/models"
)

const (
	defaultTTL = 60 * time.Second
	cleanupInt = 30 * time.Second
)

type Store struct {
	mu             sync.RWMutex
	tasks          map[string]*models.TaskState
	upstreams      map[string]*models.UpstreamState
	ttl            time.Duration
	activeTTL      time.Duration
	subscribers    []*subscription
	currentTaskKey string
	ctx            context.Context
	cancel         context.CancelFunc
}

type subscription struct {
	ch chan *models.MetricEvent
}

func NewStore(ctx context.Context, ttl, activeTTL time.Duration) *Store {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if activeTTL <= 0 {
		activeTTL = 10 * time.Minute
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Store{
		tasks:       make(map[string]*models.TaskState),
		upstreams:   make(map[string]*models.UpstreamState),
		ttl:         ttl,
		activeTTL:   activeTTL,
		subscribers: make([]*subscription, 0),
		ctx:         ctx,
		cancel:      cancel,
	}
	go s.cleanupLoop(ctx)
	return s
}

func (s *Store) Stop() {
	s.cancel()
}

func (s *Store) Subscribe() chan *models.MetricEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *models.MetricEvent, 1)
	sub := &subscription{
		ch:   ch,
	}
	s.subscribers = append(s.subscribers, sub)

	return ch
}

func (s *Store) Unsubscribe(ch chan *models.MetricEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subscribers {
		if sub.ch == ch {
			close(sub.ch)
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			return
		}
	}
}

func (s *Store) broadcast(ev *models.MetricEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	subs := make([]*subscription, len(s.subscribers))
	copy(subs, s.subscribers)

	for _, sub := range subs {
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

func (s *Store) Update(ev *models.MetricEvent) bool {
	emitSSE := s.applyEvent(ev)
	if emitSSE {
		s.broadcast(ev)
	}
	return emitSSE
}

// ensureTask returns the task state for the event, creating it when needed.
func (s *Store) ensureTask(ev *models.MetricEvent, key, phase string) *models.TaskState {
	state, ok := s.tasks[key]
	if !ok {
		state = &models.TaskState{
			Upstream:  ev.Upstream,
			Slot:      ev.Slot,
			Task:      ev.Task,
			Phase:     phase,
			UpdatedAt: ev.Timestamp,
		}
		s.tasks[key] = state
	}
	return state
}

func (s *Store) applyEvent(ev *models.MetricEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := models.StoreKey(ev.Upstream, ev.Slot, ev.Task)

	switch ev.Type {
	case "task_started":
		s.tasks[key] = &models.TaskState{
			Upstream:  ev.Upstream,
			Slot:      ev.Slot,
			Task:      ev.Task,
			Phase:     "prefill",
			UpdatedAt: ev.Timestamp,
		}
		s.currentTaskKey = key
		return true

	case "prefill_progress":
		state := s.ensureTask(ev, key, "prefill")
		state.Phase = "prefill"
		state.PromptTokens = ev.PromptTokens
		state.Progress = ev.Progress
		state.PromptTPS = ev.PromptTPS
		state.UpdatedAt = ev.Timestamp
		s.currentTaskKey = key
		return true

	case "generation_progress":
		state := s.ensureTask(ev, key, "generation")
		state.GeneratedTokens = ev.GeneratedTokens
		state.GenerationTPS = ev.GenerationTPS
		state.GenerationTPS3s = ev.GenerationTPS3s
		if state.PromptTokens > 0 {
			state.Progress = float64(ev.GeneratedTokens) / float64(state.PromptTokens+ev.GeneratedTokens)
		}
		state.Phase = "generation"
		state.UpdatedAt = ev.Timestamp
		s.currentTaskKey = key
		return true

	case "generation_complete":
		state := s.ensureTask(ev, key, "generation")
		state.GeneratedTokens = ev.GeneratedTokens
		state.GenerationTPS = ev.GenerationTPS
		state.UpdatedAt = ev.Timestamp
		return true

	case "task_finished":
		state := s.ensureTask(ev, key, "finished")
		state.Finished = true
		state.Truncated = ev.Truncated
		state.UpdatedAt = ev.Timestamp
		return true

	case "task_cancelled":
		for k, st := range s.tasks {
			if st.Task == ev.Task && st.Upstream == ev.Upstream {
				st.Cancelled = true
				st.Finished = true
				st.UpdatedAt = ev.Timestamp
				delete(s.tasks, k)
				break
			}
		}
		return true

	case "prompt_eval_result":
		state := s.ensureTask(ev, key, "prefill")
		state.PromptEvalMs = ev.PromptEvalMs
		if ev.PromptTokens > 0 {
			state.PromptTokens = ev.PromptTokens
		}
		state.PromptTPS = ev.PromptTPS
		state.UpdatedAt = ev.Timestamp
		return false

	case "total_result":
		state := s.ensureTask(ev, key, "")
		state.TotalMs = ev.TotalMs
		state.TotalTokens = ev.TotalTokens
		state.UpdatedAt = ev.Timestamp
		return true

	case "graphs_reused":
		state := s.ensureTask(ev, key, "")
		state.GraphsReused = ev.GraphsReused
		state.UpdatedAt = ev.Timestamp
		return false

	case "slot_selected":
		state := s.ensureTask(ev, key, "prefill")
		state.Keep = ev.Keep
		state.UpdatedAt = ev.Timestamp
		s.currentTaskKey = key
		return true
	}

	return false
}

// UpdateUpstream replaces the state of a polled engine and broadcasts an
// upstream_state SSE event.
func (s *Store) UpdateUpstream(us *models.UpstreamState) {
	s.mu.Lock()
	s.upstreams[us.Upstream] = us
	s.mu.Unlock()

	s.broadcast(&models.MetricEvent{
		Type:          "upstream_state",
		Upstream:      us.Upstream,
		UpstreamState: us,
		Timestamp:     us.UpdatedAt,
	})
}

// RemoveUpstream forgets a polled engine (e.g. it was unloaded).
func (s *Store) RemoveUpstream(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.upstreams, name)
}

type Snapshot struct {
	Active      bool                     `json:"active"`
	CurrentTask *models.TaskState        `json:"current_task,omitempty"`
	Tasks       []*models.TaskState      `json:"tasks"`
	Upstreams   []*models.UpstreamState  `json:"upstreams"`
}

func (s *Store) CurrentSnapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := Snapshot{
		Active:    false,
		Tasks:     make([]*models.TaskState, 0, len(s.tasks)),
		Upstreams: make([]*models.UpstreamState, 0, len(s.upstreams)),
	}

	if ct, ok := s.tasks[s.currentTaskKey]; ok && ct != nil {
		snapshot.CurrentTask = cloneState(ct)
		snapshot.Active = !ct.Finished && !ct.Cancelled
	}

	for _, st := range s.tasks {
		snapshot.Tasks = append(snapshot.Tasks, cloneState(st))
	}

	for _, us := range s.upstreams {
		c := *us
		snapshot.Upstreams = append(snapshot.Upstreams, &c)
	}

	sort.Slice(snapshot.Upstreams, func(i, j int) bool {
		return snapshot.Upstreams[i].Upstream < snapshot.Upstreams[j].Upstream
	})

	return snapshot
}

func cloneState(s *models.TaskState) *models.TaskState {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func (s *Store) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(cleanupInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *Store) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, st := range s.tasks {
		var ttl time.Duration
		if st.Finished || st.Cancelled {
			ttl = s.ttl
		} else {
			ttl = s.activeTTL
		}
		if now.Sub(st.UpdatedAt) > ttl {
			delete(s.tasks, key)
		}
	}

	for key, us := range s.upstreams {
		if now.Sub(us.UpdatedAt) > s.ttl {
			delete(s.upstreams, key)
		}
	}
}
