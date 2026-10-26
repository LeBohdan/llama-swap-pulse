// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"llama-swap-pulse/internal/models"
)

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[
			{"id":"Strata-Model","object":"model","status":{"value":"loaded"}},
			{"id":"Cold-Model","object":"model","status":{"value":"unloaded"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	list, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 models, got %d", len(list))
	}
	if list[0].ID != "Strata-Model" || !list[0].Loaded {
		t.Errorf("unexpected first model: %+v", list[0])
	}
	if list[1].ID != "Cold-Model" || list[1].Loaded {
		t.Errorf("unexpected second model: %+v", list[1])
	}
}

func TestProbeClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upstream/Strata-M/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "loaded": true, "service": "strata"})
		case "/upstream/Maya-M/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "loaded": true, "service": "maya"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx := context.Background()

	if kind, err := c.Probe(ctx, "Strata-M"); err != nil || kind != models.EngineStrata {
		t.Errorf("strata probe: kind=%s err=%v", kind, err)
	}
	if kind, err := c.Probe(ctx, "Maya-M"); err != nil || kind != models.EngineMaya {
		t.Errorf("maya probe: kind=%s err=%v", kind, err)
	}
	if kind, err := c.Probe(ctx, "LLAMA-M"); kind != models.EngineUnknown {
		t.Errorf("expected unknown for 404, got kind=%s err=%v", kind, err)
	}
}

func TestProbeUsesCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]any{"service": "strata"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx := context.Background()
	c.Probe(ctx, "M")
	c.Probe(ctx, "M")
	if hits != 1 {
		t.Errorf("expected 1 health probe (cached), got %d", hits)
	}
	c.ForgetProbe("M")
	c.Probe(ctx, "M")
	if hits != 2 {
		t.Errorf("expected probe after ForgetProbe, got %d hits", hits)
	}
}
