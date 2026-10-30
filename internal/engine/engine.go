// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"llama-swap-pulse/internal/models"
)

// ProcessStateReady is llama-swap's /running state for a live, healthy
// upstream process. Only models in this state may be polled.
const ProcessStateReady = "ready"

// RunningModel is one non-stopped process reported by llama-swap GET /running.
// Proxy and Cmd carry the model config as written (llama-swap does not
// resolve ${PORT} there), which is what engineBaseURL parses.
type RunningModel struct {
	ID    string
	State string
	Proxy string
	Cmd   string
}

// engineBaseURL derives the engine's direct HTTP base URL from llama-swap's
// /running entry. Polling engines directly (instead of through
// <swap>/upstream/<id>/...) is mandatory: the /upstream route runs through
// llama-swap's swap logic and (re)starts models that are stopping or stopped,
// causing reload loops and keeping model ttl from ever expiring.
//
// The engine lives on the same machine as llama-swap, so loopback/unspecified
// hosts (localhost, 127.0.0.1, 0.0.0.0, ...) in proxy:/cmd are rewritten to
// swapHost — the host of the configured llama-swap URL. That keeps direct
// polling working when pulse runs on a different machine than the swap.
//
// Returns "" when no direct address is observable (no proxy: entry and no
// literal port in cmd, e.g. cmd uses ${PORT}); such engines stay log-only.
func engineBaseURL(rm RunningModel, swapHost string) string {
	if p := strings.TrimSpace(rm.Proxy); p != "" {
		return rewriteProxyURL(p, swapHost)
	}

	var port, host string
	fields := strings.Fields(rm.Cmd) // cmd may embed newlines between args
	for i := 0; i < len(fields); i++ {
		name, val, hasVal := strings.Cut(fields[i], "=")
		if !hasVal && name != "--port" && name != "-port" && name != "--host" && name != "-host" {
			continue
		}
		if !hasVal {
			if i+1 >= len(fields) {
				continue
			}
			i++
			val = fields[i]
		}
		switch name {
		case "--port", "-port":
			port = val
		case "--host", "-host":
			host = val
		}
	}

	if !allDigits(port) { // empty, ${PORT}, or anything non-numeric
		return ""
	}
	if isLocalHost(host) {
		host = outboundHost(swapHost)
	}
	return "http://" + host + ":" + port
}

// rewriteProxyURL normalizes a proxy: URL from llama-swap model config,
// substituting its loopback host with swapHost. Malformed values are passed
// through trimmed of the trailing slash.
func rewriteProxyURL(p, swapHost string) string {
	u, err := url.Parse(strings.TrimRight(p, "/"))
	if err != nil || u.Host == "" {
		return strings.TrimRight(p, "/")
	}
	if isLocalHost(u.Hostname()) {
		u.Host = net.JoinHostPort(outboundHost(swapHost), u.Port())
	}
	return u.String()
}

// isLocalHost reports hosts that only reach the machine llama-swap runs on.
func isLocalHost(host string) bool {
	switch host {
	case "", "*", "0.0.0.0", "::", "[::]", "::1", "[::1]", "localhost", "127.0.0.1":
		return true
	}
	return false
}

// outboundHost is the host to dial for loopback engine addresses: the
// llama-swap host itself, or localhost when the swap is local too.
func outboundHost(swapHost string) string {
	if swapHost == "" || isLocalHost(swapHost) {
		return "localhost"
	}
	return swapHost
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Client talks to the llama-swap HTTP API and (for engine metrics) directly
// to the upstream engine processes on their own ports.
type Client struct {
	baseURL  string
	swapHost string // host part of baseURL, for loopback engine URL rewrites
	http     *http.Client

	mu     sync.Mutex
	probes map[string]string
}

func NewClient(baseURL string) *Client {
	c := &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{IdleConnTimeout: 90 * time.Second, MaxIdleConnsPerHost: 4},
		},
		probes: make(map[string]string),
	}
	if u, err := url.Parse(baseURL); err == nil {
		c.swapHost = u.Hostname()
	}
	return c
}

// EngineURL resolves the direct base URL for a running model entry.
func (c *Client) EngineURL(rm RunningModel) string {
	return engineBaseURL(rm, c.swapHost)
}

// swapGetJSON fetches a llama-swap API endpoint (relative path against the
// configured swap base URL).
func (c *Client) swapGetJSON(ctx context.Context, path string, out any) error {
	return c.getJSON(ctx, c.baseURL+path, out)
}

// getJSON fetches and decodes an absolute URL.
func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// ListRunning returns the models llama-swap currently has running (in any
// non-stopped state) along with their process state and config text. This
// endpoint never triggers a swap, unlike the /upstream/... proxy.
func (c *Client) ListRunning(ctx context.Context) ([]RunningModel, error) {
	var payload struct {
		Running []struct {
			Model string `json:"model"`
			State string `json:"state"`
			Proxy string `json:"proxy"`
			Cmd   string `json:"cmd"`
		} `json:"running"`
	}
	if err := c.swapGetJSON(ctx, "/running", &payload); err != nil {
		return nil, err
	}
	list := make([]RunningModel, 0, len(payload.Running))
	for _, m := range payload.Running {
		if m.Model == "" {
			continue
		}
		list = append(list, RunningModel{ID: m.Model, State: m.State, Proxy: m.Proxy, Cmd: m.Cmd})
	}
	return list, nil
}

// Probe classifies an engine by its direct /health payload. The "service"
// field is present only for Strata ("strata") and Maya ("maya"); llama.cpp
// has no web-UI health endpoint with that field, so it maps to unknown.
// Unknown results are not cached so they are retried on the next cycle.
func (c *Client) Probe(ctx context.Context, id, baseURL string) (string, error) {
	c.mu.Lock()
	if eng, ok := c.probes[id]; ok {
		c.mu.Unlock()
		return eng, nil
	}
	c.mu.Unlock()

	var health struct {
		Service string `json:"service"`
	}
	if err := c.getJSON(ctx, baseURL+"/health", &health); err != nil {
		return models.EngineUnknown, err
	}

	switch health.Service {
	case models.EngineStrata:
		return c.cacheProbe(id, models.EngineStrata), nil
	case models.EngineMaya:
		return c.cacheProbe(id, models.EngineMaya), nil
	default:
		return models.EngineUnknown, nil
	}
}

func (c *Client) cacheProbe(id, engine string) string {
	c.mu.Lock()
	c.probes[id] = engine
	c.mu.Unlock()
	return engine
}

// ForgetProbe drops the cached classification for an upstream id.
func (c *Client) ForgetProbe(id string) {
	c.mu.Lock()
	delete(c.probes, id)
	c.mu.Unlock()
}

// FetchMetrics reads the current engine metrics snapshot (live state,
// hardware telemetry) directly from the engine. The requests history is
// ignored.
func (c *Client) FetchMetrics(ctx context.Context, baseURL string) (*Metrics, error) {
	var m Metrics
	if err := c.getJSON(ctx, baseURL+"/metrics", &m); err != nil {
		return nil, err
	}
	return &m, nil
}
