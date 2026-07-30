// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package metrics

import (
	"context"
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

func (s *Store) applyEvent(ev *models.MetricEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := models.TaskKey(ev.Slot, ev.Task)

	switch ev.Type {
	case "task_started":
		s.tasks[key] = &models.TaskState{
			Slot:      ev.Slot,
			Task:      ev.Task,
			Phase:     "prefill",
			UpdatedAt: ev.Timestamp,
		}
		s.currentTaskKey = key
		return true

	case "prefill_progress":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "prefill",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.Phase = "prefill"
		state.PromptTokens = ev.PromptTokens
		state.Progress = ev.Progress
		state.PromptTPS = ev.PromptTPS
		state.UpdatedAt = ev.Timestamp
		s.currentTaskKey = key
		return true

	case "generation_progress":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "generation",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
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
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "generation",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.GeneratedTokens = ev.GeneratedTokens
		state.GenerationTPS = ev.GenerationTPS
		state.UpdatedAt = ev.Timestamp
		return true

	case "task_finished":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "finished",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.Finished = true
		state.Truncated = ev.Truncated
		state.UpdatedAt = ev.Timestamp
		return true

	case "task_cancelled":
		for k, st := range s.tasks {
			if st.Task == ev.Task {
				st.Cancelled = true
				st.Finished = true
				st.UpdatedAt = ev.Timestamp
				delete(s.tasks, k)
				break
			}
		}
		return true

	case "prompt_eval_result":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "prefill",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.PromptEvalMs = ev.PromptEvalMs
		if ev.PromptTokens > 0 {
			state.PromptTokens = ev.PromptTokens
		}
		state.PromptTPS = ev.PromptTPS
		state.UpdatedAt = ev.Timestamp
		return false

	case "total_result":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.TotalMs = ev.TotalMs
		state.TotalTokens = ev.TotalTokens
		state.UpdatedAt = ev.Timestamp
		return true

	case "graphs_reused":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.GraphsReused = ev.GraphsReused
		state.UpdatedAt = ev.Timestamp
		return false

	case "slot_selected":
		state, ok := s.tasks[key]
		if !ok {
			state = &models.TaskState{
				Slot:      ev.Slot,
				Task:      ev.Task,
				Phase:     "prefill",
				UpdatedAt: ev.Timestamp,
			}
			s.tasks[key] = state
		}
		state.Keep = ev.Keep
		state.UpdatedAt = ev.Timestamp
		s.currentTaskKey = key
		return true
	}

	return false
}

type Snapshot struct {
	Active      bool                `json:"active"`
	CurrentTask *models.TaskState   `json:"current_task,omitempty"`
	Tasks       []*models.TaskState `json:"tasks"`
}

func (s *Store) CurrentSnapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := Snapshot{
		Active: false,
		Tasks:  make([]*models.TaskState, 0, len(s.tasks)),
	}

	if ct, ok := s.tasks[s.currentTaskKey]; ok && ct != nil {
		snapshot.CurrentTask = cloneState(ct)
		snapshot.Active = !ct.Finished && !ct.Cancelled
	}

	for _, st := range s.tasks {
		snapshot.Tasks = append(snapshot.Tasks, cloneState(st))
	}

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
}
