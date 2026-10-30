// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

// Package engine discovers Strata/Maya upstreams behind llama-swap and polls
// their web-UI /metrics endpoint directly (on the engine's own port) for live
// prefill/generation metrics. It never routes through llama-swap's
// /upstream/... proxy because that proxy (re)starts non-ready processes.
package engine

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"llama-swap-pulse/internal/metrics"
	"llama-swap-pulse/internal/models"
)

// Manager keeps one poller goroutine per loaded Strata/Maya upstream and a
// discovery loop that tracks llama-swap's running-process list automatically.
// All engine traffic is direct (engine port), never through llama-swap's
// /upstream/... proxy, which would (re)start processes that are stopping.
type Manager struct {
	client  *Client
	store   *metrics.Store
	log     *slog.Logger
	poll    time.Duration
	discEvery time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	slotSeq atomic.Int64

	mu         sync.Mutex
	pollers    map[string]*poller
	warnedNoURL map[string]bool
}

// poller owns the per-upstream task state machine. Its state fields are only
// touched from the poll goroutine.
type poller struct {
	id     string
	engine string
	base   string // engine direct base URL (never the swap proxy)
	slot   int
	cancel context.CancelFunc

	task       int  // id of the current/last synthetic task
	taskSeq    int  // monotonically increasing per-upstream task counter
	active     bool // a task is in flight
	lastPrompt int
	lastGen    int
	lastGenTPS float64
	reading    bool // saw "reading" for the current task
	lastReadMs float64
	fails      int // consecutive /metrics failures
}

// maxPollFails stops a poller after this many consecutive direct /metrics
// errors (engine crashed or port changed) instead of hammering a dead port
// until the next discovery tick.
const maxPollFails = 3

func NewManager(baseURL string, store *metrics.Store, poll, discovery time.Duration, log *slog.Logger) *Manager {
	if poll <= 0 {
		poll = time.Second
	}
	if discovery <= 0 {
		discovery = 5 * time.Second
	}
	return &Manager{
		client:      NewClient(baseURL),
		store:       store,
		log:         log,
		poll:        poll,
		discEvery:   discovery,
		pollers:     make(map[string]*poller),
		warnedNoURL: make(map[string]bool),
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.wg.Add(1)
	go m.discoveryLoop()
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Manager) discoveryLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.discEvery)
	defer ticker.Stop()

	m.discover()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.discover()
		}
	}
}

func (m *Manager) discover() {
	list, err := m.client.ListRunning(m.ctx)
	if err != nil {
		m.log.Debug("engine discovery failed", "err", err)
		return
	}

	ready := make(map[string]RunningModel, len(list))
	for _, rm := range list {
		if rm.State == ProcessStateReady {
			ready[rm.ID] = rm
		}
	}

	m.mu.Lock()
	// Stop pollers for models llama-swap no longer reports as running ready.
	for id, p := range m.pollers {
		if _, ok := ready[id]; !ok {
			p.cancel()
			delete(m.pollers, id)
		}
	}

	// Start pollers for newly ready Strata/Maya engines. Probing only ready
	// models and never via the /upstream proxy avoids waking or restarting
	// engines llama-swap is stopping.
	for id, rm := range ready {
		if _, exists := m.pollers[id]; exists {
			continue
		}
		base := m.client.EngineURL(rm)
		if base == "" {
			if !m.warnedNoURL[id] {
				m.log.Warn("cannot resolve engine direct URL (no proxy and no literal port in cmd); staying log-only", "upstream", id)
				m.warnedNoURL[id] = true
			}
			continue
		}
		delete(m.warnedNoURL, id)
		kind, err := m.client.Probe(m.ctx, id, base)
		if err != nil || (kind != models.EngineStrata && kind != models.EngineMaya) {
			continue
		}
		m.startPoller(id, kind, base)
	}
	m.mu.Unlock()
}

func (m *Manager) startPoller(id, kind, base string) {
	p := &poller{
		id:     id,
		engine: kind,
		base:   base,
		slot:   int(m.slotSeq.Add(1)),
	}
	pctx, cancel := context.WithCancel(m.ctx)
	p.cancel = cancel
	m.pollers[id] = p

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.pollLoop(pctx, p)
	}()
	m.log.Info("engine poller started", "upstream", id, "engine", kind, "url", base)
}

func (m *Manager) pollLoop(ctx context.Context, p *poller) {
	ticker := time.NewTicker(m.poll)
	defer ticker.Stop()

	// Closing the in-flight task here keeps task lifecycle complete for every
	// exit path: model unload, repeated metrics failures, manager stop.
	defer func() {
		m.completeTask(p)
		m.store.RemoveUpstream(p.id)
		m.client.ForgetProbe(p.id)
		m.log.Info("engine poller stopped", "upstream", p.id)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		mx, err := m.client.FetchMetrics(ctx, p.base)
		if err != nil {
			if ctx.Err() != nil {
				return // manager shutting down, not an engine failure
			}
			p.fails++
			if p.fails >= maxPollFails {
				m.log.Warn("engine poller stopping after repeated metrics failures",
					"upstream", p.id, "err", err)
				return
			}
			m.log.Debug("engine metrics failed", "upstream", p.id, "err", err)
			continue
		}
		p.fails = 0

		m.store.UpdateUpstream(upstreamState(p, mx))
		m.drive(p, mx.Live)
	}
}

// drive turns a live snapshot into task lifecycle events. Only the current
// snapshot is used; request history is ignored by design.
func (m *Manager) drive(p *poller, live Live) {
	switch live.State {
	case models.StateReading:
		m.startTask(p)
		p.reading = true
		p.lastReadMs = live.ElapsedS * 1000
		p.lastPrompt = live.PromptTokens

		ev := m.newEvent(p, "prefill_progress")
		ev.PromptTokens = live.PromptTokens
		if live.PromptRead != nil && live.PromptTotal != nil && *live.PromptTotal > 0 {
			ev.Progress = float64(*live.PromptRead) / float64(*live.PromptTotal)
		}
		if live.PrefillTokSMean != nil {
			ev.PromptTPS = *live.PrefillTokSMean
		}
		m.store.Update(ev)

	case models.StateGenerating:
		m.startTask(p)
		p.lastPrompt = live.PromptTokens
		p.lastGen = live.Generated

		// First generating tick after reading: report prefill result.
		if p.reading {
			p.reading = false
			ev := m.newEvent(p, "prompt_eval_result")
			ev.PromptTokens = live.PromptTokens
			if live.PrefillTokSMean != nil && *live.PrefillTokSMean > 0 {
				ev.PromptTPS = *live.PrefillTokSMean
				if p.lastReadMs == 0 && live.PromptTokens > 0 {
					p.lastReadMs = float64(live.PromptTokens) / *live.PrefillTokSMean * 1000
				}
			}
			ev.PromptEvalMs = p.lastReadMs
			m.store.Update(ev)
		}

		ev := m.newEvent(p, "generation_progress")
		ev.PromptTokens = live.PromptTokens
		ev.GeneratedTokens = live.Generated
		if live.TokS != nil {
			ev.GenerationTPS = *live.TokS
			p.lastGenTPS = *live.TokS
		}
		if live.TokSMean != nil {
			ev.GenerationTPS3s = *live.TokSMean
		}
		m.store.Update(ev)

	case models.StateQueued:
		// Queue depth is reported via upstream_state; if a task was in
		// flight the engine has already moved on.
		if p.active {
			m.completeTask(p)
		}

	default: // idle, unloaded, unknown
		if p.active {
			m.completeTask(p)
		}
	}
}

func (m *Manager) startTask(p *poller) {
	if p.active {
		return
	}
	p.taskSeq++
	p.task = p.taskSeq
	p.active = true
	p.reading = false
	p.lastPrompt = 0
	p.lastGen = 0
	p.lastGenTPS = 0
	p.lastReadMs = 0

	ev := m.newEvent(p, "task_started")
	m.store.Update(ev)
}

func (m *Manager) completeTask(p *poller) {
	if !p.active {
		return
	}

	ev := m.newEvent(p, "generation_complete")
	ev.PromptTokens = p.lastPrompt
	ev.GeneratedTokens = p.lastGen
	ev.NTokens = p.lastGen
	ev.GenerationTPS = p.lastGenTPS
	m.store.Update(ev)

	fin := m.newEvent(p, "task_finished")
	m.store.Update(fin)

	p.active = false
	p.reading = false
}

func (m *Manager) newEvent(p *poller, typ string) *models.MetricEvent {
	return &models.MetricEvent{
		Type:      typ,
		Upstream:  p.id,
		Slot:      p.slot,
		Task:      p.task,
		Timestamp: time.Now(),
	}
}

func upstreamState(p *poller, mx *Metrics) *models.UpstreamState {
	us := &models.UpstreamState{
		Upstream:  p.id,
		Engine:    p.engine,
		State:     mx.Live.State,
		Phase:     mx.Live.Phase,
		Queued:    mx.Live.Queued,
		Model:     mx.Engine.Model,
		CPU:       mx.Hardware.CPU,
		UpdatedAt: time.Now(),
	}

	gpu := models.GPUStats{
		Util:     mx.Hardware.GPUUtil,
		MemUsed:  mx.Hardware.GPUMemUsed,
		MemTotal: mx.Hardware.GPUMemTotal,
		Temp:     mx.Hardware.GPUTemp,
		Power:    mx.Hardware.GPUPower,
	}
	us.GPU = &gpu

	if mx.Hardware.RAMTotal > 0 || mx.Hardware.RAMUsed > 0 {
		us.RAM = &models.RAMStats{Used: mx.Hardware.RAMUsed, Total: mx.Hardware.RAMTotal}
	}

	return us
}
