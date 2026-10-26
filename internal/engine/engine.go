// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"llama-swap-pulse/internal/models"
)

// Model is one upstream model known to llama-swap.
type Model struct {
	ID     string
	Loaded bool
}

// Client talks to the llama-swap HTTP API and to the proxied upstream
// engines (Strata/Maya web-UI endpoints under /upstream/<id>/...).
type Client struct {
	baseURL string
	http    *http.Client

	mu     sync.Mutex
	probes map[string]string
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{IdleConnTimeout: 90 * time.Second, MaxIdleConnsPerHost: 4},
		},
		probes: make(map[string]string),
	}
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// ListModels returns all models configured in llama-swap along with their
// loaded state (auto-discovery, no manual configuration needed).
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	var payload struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, "/v1/models", &payload); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID == "" {
			continue
		}
		models = append(models, Model{ID: m.ID, Loaded: m.Status.Value == "loaded"})
	}
	return models, nil
}

// upstreamPath escapes the upstream id for the llama-swap proxy route.
func upstreamPath(id string) string {
	return "/upstream/" + url.PathEscape(id)
}

// Probe classifies an upstream engine by its /health payload. The "service"
// field is present only for Strata ("strata") and Maya ("maya"); llama.cpp
// has no web-UI health endpoint with that field, so it maps to llamacpp.
// Unknown results are not cached so they are retried on the next cycle.
func (c *Client) Probe(ctx context.Context, id string) (string, error) {
	c.mu.Lock()
	if eng, ok := c.probes[id]; ok {
		c.mu.Unlock()
		return eng, nil
	}
	c.mu.Unlock()

	var health struct {
		Service string `json:"service"`
	}
	if err := c.getJSON(ctx, upstreamPath(id)+"/health", &health); err != nil {
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
// hardware telemetry). The requests history is ignored.
func (c *Client) FetchMetrics(ctx context.Context, id string) (*Metrics, error) {
	var m Metrics
	if err := c.getJSON(ctx, upstreamPath(id)+"/metrics", &m); err != nil {
		return nil, err
	}
	return &m, nil
}
