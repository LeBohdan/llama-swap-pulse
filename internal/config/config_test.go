// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Listen != ":8090" {
		t.Errorf("expected listen :8090, got %s", cfg.Server.Listen)
	}
	if cfg.LlamaSwap.URL != "http://localhost:8080" {
		t.Errorf("expected url http://localhost:8080, got %s", cfg.LlamaSwap.URL)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("expected level info, got %s", cfg.Logging.Level)
	}
	if cfg.MetricsTTL != 60*time.Second {
		t.Errorf("expected ttl 60s, got %v", cfg.MetricsTTL)
	}
	if cfg.MetricsActiveTTL != 10*time.Minute {
		t.Errorf("expected activeTTL 10m, got %v", cfg.MetricsActiveTTL)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	content := `server:
  listen: ":9090"

 llama_swap:
  url: "http://my-swap:8080"

 logging:
  level: debug

 metrics:
  ttl: 120s
  active_ttl: 20m
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Server.Listen)
	}
	if cfg.LlamaSwap.URL != "http://my-swap:8080" {
		t.Errorf("expected url http://my-swap:8080, got %s", cfg.LlamaSwap.URL)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected level debug, got %s", cfg.Logging.Level)
	}
	if cfg.MetricsTTL != 120*time.Second {
		t.Errorf("expected ttl 120s, got %v", cfg.MetricsTTL)
	}
	if cfg.MetricsActiveTTL != 20*time.Minute {
		t.Errorf("expected activeTTL 20m, got %v", cfg.MetricsActiveTTL)
	}
}

func TestEnvOverrides(t *testing.T) {
	os.Setenv("SERVER_LISTEN", ":7777")
	os.Setenv("LLAMA_SWAP_URL", "http://remote-swap:9999")
	os.Setenv("LOGGING_LEVEL", "error")
	os.Setenv("METRICS_ACTIVE_TTL", "15m")
	defer os.Unsetenv("SERVER_LISTEN")
	defer os.Unsetenv("LLAMA_SWAP_URL")
	defer os.Unsetenv("LOGGING_LEVEL")
	defer os.Unsetenv("METRICS_ACTIVE_TTL")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Listen != ":7777" {
		t.Errorf("expected listen :7777, got %s", cfg.Server.Listen)
	}
	if cfg.LlamaSwap.URL != "http://remote-swap:9999" {
		t.Errorf("expected url http://remote-swap:9999, got %s", cfg.LlamaSwap.URL)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("expected level error, got %s", cfg.Logging.Level)
	}
	if cfg.MetricsActiveTTL != 15*time.Minute {
		t.Errorf("expected activeTTL 15m, got %v", cfg.MetricsActiveTTL)
	}
}

func TestMetricsTTL(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MetricsTTL != 60*time.Second {
		t.Errorf("expected ttl 60s, got %v", cfg.MetricsTTL)
	}
}

func TestMetricsActiveTTL(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MetricsActiveTTL != 10*time.Minute {
		t.Errorf("expected activeTTL 10m, got %v", cfg.MetricsActiveTTL)
	}
}
