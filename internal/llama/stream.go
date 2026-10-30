// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

// Package llama keeps one SSE log stream per loaded llama.cpp model. Each
// stream is a llama-swap /logs/stream/{model_id} connection (?no-history),
// so every log line is attributed to its model. Models covered by a polled
// engine (Strata/Maya) do not get a log stream.
package llama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// StreamManager tracks loaded llama.cpp models and streams their logs.
type StreamManager interface {
	Start(ctx context.Context) error
	Stop()
	Wait()
	Subscribe(func(modelID, line string))
	Connected() bool
}

type manager struct {
	baseURL   string
	discEvery time.Duration
	// covered returns the set of model ids handled by an engine poller
	// (Strata/Maya); those models must not get a log stream.
	covered   func() map[string]bool
	log       *slog.Logger
	sseClient *http.Client
	hcClient  *http.Client

	mu        sync.RWMutex
	subs      []func(string, string)
	streams   map[string]context.CancelFunc
	connected bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func New(baseURL string, discovery time.Duration, log *slog.Logger, covered func() map[string]bool) StreamManager {
	if discovery <= 0 {
		discovery = 5 * time.Second
	}
	if covered == nil {
		covered = func() map[string]bool { return nil }
	}
	transport := &http.Transport{
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 2,
	}
	return &manager{
		baseURL:   baseURL,
		discEvery: discovery,
		covered:   covered,
		log:       log,
		sseClient: &http.Client{Transport: transport},
		hcClient:  &http.Client{Timeout: 10 * time.Second, Transport: transport},
		streams:   make(map[string]context.CancelFunc),
	}
}

func (m *manager) Subscribe(fn func(string, string)) {
	m.mu.Lock()
	m.subs = append(m.subs, fn)
	m.mu.Unlock()
}

func (m *manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return fmt.Errorf("already started")
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	m.wg.Add(2)
	go m.healthLoop(ctx)
	go m.discoveryLoop(ctx)
	return nil
}

func (m *manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
}

func (m *manager) Wait() {
	m.wg.Wait()
}

func (m *manager) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *manager) setConnected(v bool) {
	m.mu.Lock()
	m.connected = v
	m.mu.Unlock()
}

func (m *manager) broadcast(modelID, line string) {
	m.mu.RLock()
	subs := make([]func(string, string), len(m.subs))
	copy(subs, m.subs)
	m.mu.RUnlock()

	for _, fn := range subs {
		fn(modelID, line)
	}
}

func (m *manager) healthLoop(ctx context.Context) {
	defer m.wg.Done()

	m.checkHealth()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHealth()
		}
	}
}

// checkHealth probes llama-swap's /health endpoint. It is the sole source of
// the Connected() state: no per-stream connection state is involved, so the
// health check stays meaningful even when zero models are loaded.
func (m *manager) checkHealth() {
	req, err := http.NewRequestWithContext(context.Background(), "GET", m.baseURL+"/health", nil)
	if err != nil {
		m.setConnected(false)
		return
	}
	resp, err := m.hcClient.Do(req)
	if err != nil {
		m.setConnected(false)
		m.log.Warn("llama-swap health-check failed", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.setConnected(false)
		m.log.Warn("llama-swap health-check non-200", "status", resp.Status)
		return
	}
	m.setConnected(true)
}

func (m *manager) discoveryLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.discEvery)
	defer ticker.Stop()

	m.discover(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.discover(ctx)
		}
	}
}

func (m *manager) discover(ctx context.Context) {
	loaded, err := m.listLoadedModels(ctx)
	if err != nil {
		m.log.Debug("llama.cpp model discovery failed", "err", err)
		return
	}
	covered := m.covered()

	m.mu.Lock()
	// Close streams for models that are no longer loaded or have become
	// covered by an engine poller.
	for id, cancel := range m.streams {
		if !loaded[id] || covered[id] {
			cancel()
			delete(m.streams, id)
			m.log.Info("log stream closed", "model", id)
		}
	}
	// Open streams for newly loaded llama.cpp models.
	for id := range loaded {
		if covered[id] {
			continue
		}
		if _, ok := m.streams[id]; ok {
			continue
		}
		sctx, cancel := context.WithCancel(ctx)
		m.streams[id] = cancel
		m.wg.Add(1)
		go m.streamLoop(sctx, id)
		m.log.Info("log stream opened", "model", id)
	}
	m.mu.Unlock()
}

func (m *manager) streamLoop(ctx context.Context, id string) {
	defer m.wg.Done()
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		err := m.readStream(ctx, id)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.log.Debug("model log stream error, reconnecting", "model", id, "err", err, "backoff", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (m *manager) readStream(ctx context.Context, id string) error {
	// ?no-history skips the buffered process history: pulse only needs new
	// lines, and the history contains pre-load (possibly failed) attempts.
	u := m.baseURL + "/logs/stream/" + url.PathEscape(id) + "?no-history"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := m.sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}

		m.broadcast(id, line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return fmt.Errorf("stream closed by server")
}

// listLoadedModels returns the canonical ids of models llama-swap reports as
// loaded. Aliases (meta.llamaswap.type != "model") are excluded: /logs/stream
// is keyed by the canonical model id.
func (m *manager) listLoadedModels(ctx context.Context) (map[string]bool, error) {
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Meta struct {
				LlamaSwap struct {
					Type string `json:"type"`
				} `json:"llamaswap"`
			} `json:"meta"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := m.getJSON(ctx, m.baseURL+"/v1/models", &payload); err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(payload.Data))
	for _, md := range payload.Data {
		if md.ID == "" {
			continue
		}
		if md.Meta.LlamaSwap.Type != "model" {
			continue
		}
		if md.Status.Value == "loaded" {
			out[md.ID] = true
		}
	}
	return out, nil
}

func (m *manager) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := m.hcClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
