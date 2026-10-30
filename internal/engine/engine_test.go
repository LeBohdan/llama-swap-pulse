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

func TestListRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/running" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"running":[
			{"model":"Strata-M","state":"ready","proxy":"http://localhost:8114",
			 "cmd":"/venv/bin/python serve.py\n--port 8114\n--host 0.0.0.0\n"},
			{"model":"Warming-M","state":"starting","proxy":"","cmd":"llama-server --port ${PORT}"}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	list, err := c.ListRunning(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 models, got %d", len(list))
	}
	if list[0].ID != "Strata-M" || list[0].State != ProcessStateReady || list[0].Proxy == "" {
		t.Errorf("unexpected first model: %+v", list[0])
	}
	if list[1].ID != "Warming-M" || list[1].State != "starting" {
		t.Errorf("unexpected second model: %+v", list[1])
	}
}

func TestEngineBaseURL(t *testing.T) {
	cases := []struct {
		name     string
		rm       RunningModel
		swapHost string
		want     string
	}{
		{
			name:     "proxy config wins, loopback rewritten to swap host",
			rm:       RunningModel{State: "ready", Proxy: "http://localhost:8114/", Cmd: "python serve.py --port 9999"},
			swapHost: "polaris",
			want:     "http://polaris:8114",
		},
		{
			name:     "loopback proxy kept when swap is local",
			rm:       RunningModel{State: "ready", Proxy: "http://127.0.0.1:8114"},
			swapHost: "localhost",
			want:     "http://localhost:8114",
		},
		{
			name:     "non-loopback proxy host kept",
			rm:       RunningModel{State: "ready", Proxy: "http://10.1.0.9:8114"},
			swapHost: "polaris",
			want:     "http://10.1.0.9:8114",
		},
		{
			name:     "port from multiline cmd, 0.0.0.0 rewritten",
			rm:       RunningModel{State: "ready", Cmd: "/venv/bin/python serve.py\n--port 8114\n--host 0.0.0.0\n--open\n"},
			swapHost: "polaris",
			want:     "http://polaris:8114",
		},
		{
			name:     "explicit routable host kept",
			rm:       RunningModel{State: "ready", Cmd: "maya serve -p 1 --port 8123 --host 10.9.9.9"},
			swapHost: "polaris",
			want:     "http://10.9.9.9:8123",
		},
		{
			name:     "equals form",
			rm:       RunningModel{State: "ready", Cmd: "engine --port=8134"},
			swapHost: "",
			want:     "http://localhost:8134",
		},
		{
			name:     "unresolved PORT placeholder stays empty",
			rm:       RunningModel{State: "ready", Cmd: "llama-server --port ${PORT}"},
			swapHost: "polaris",
			want:     "",
		},
		{
			name:     "no port stays empty",
			rm:       RunningModel{State: "ready", Cmd: "llama-server --model x.gguf"},
			swapHost: "polaris",
			want:     "",
		},
	}
	for _, tc := range cases {
		if got := engineBaseURL(tc.rm, tc.swapHost); got != tc.want {
			t.Errorf("%s: engineBaseURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestProbeClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/strata/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "loaded": true, "service": "strata"})
		case "/maya/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "loaded": true, "service": "maya"})
		case "/maya-legacy/health":
			// Current Maya builds report no "service" field at all.
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "loaded": true, "version": "0.4.1", "model_label": "Qwen3-8B"})
		case "/plain/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient("http://swap.invalid") // swap base must not be used for probes
	ctx := context.Background()

	if kind, err := c.Probe(ctx, "Strata-M", srv.URL+"/strata"); err != nil || kind != models.EngineStrata {
		t.Errorf("strata probe: kind=%s err=%v", kind, err)
	}
	if kind, err := c.Probe(ctx, "Maya-M", srv.URL+"/maya"); err != nil || kind != models.EngineMaya {
		t.Errorf("maya probe: kind=%s err=%v", kind, err)
	}
	if kind, err := c.Probe(ctx, "MayaLegacy-M", srv.URL+"/maya-legacy"); err != nil || kind != models.EngineMaya {
		t.Errorf("maya probe without service field: kind=%s err=%v", kind, err)
	}
	kind, err := c.Probe(ctx, "LLAMA-M", srv.URL+"/plain")
	if kind != models.EngineUnknown || err != nil {
		t.Errorf("expected unknown without service field, got kind=%s err=%v", kind, err)
	}
	if kind, err := c.Probe(ctx, "Gone-M", srv.URL+"/missing"); kind != models.EngineUnknown || err == nil {
		t.Errorf("expected error+unknown for 404, got kind=%s err=%v", kind, err)
	}
}

func TestProbeUsesCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]any{"service": "strata"})
	}))
	defer srv.Close()

	c := NewClient("http://swap.invalid")
	ctx := context.Background()
	c.Probe(ctx, "M", srv.URL)
	c.Probe(ctx, "M", srv.URL)
	if hits != 1 {
		t.Errorf("expected 1 health probe (cached), got %d", hits)
	}
	c.ForgetProbe("M")
	c.Probe(ctx, "M", srv.URL)
	if hits != 2 {
		t.Errorf("expected probe after ForgetProbe, got %d hits", hits)
	}
}
