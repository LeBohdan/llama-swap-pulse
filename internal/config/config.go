// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server           ServerConfig
	LlamaSwap        LlamaSwapConfig
	Logging          LoggingConfig
	MetricsTTL       time.Duration
	MetricsActiveTTL time.Duration
}

type ServerConfig struct {
	Listen string
}

type LlamaSwapConfig struct {
	URL string
}

type LoggingConfig struct {
	Level string
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Listen: ":8090",
		},
		LlamaSwap: LlamaSwapConfig{
			URL: "http://localhost:8080",
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		MetricsTTL:       60 * time.Second,
		MetricsActiveTTL: 10 * time.Minute,
	}
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		if err := parseFile(path, &cfg); err != nil {
			return cfg, err
		}
	}

	applyEnvOverrides(&cfg)

	return cfg, nil
}

func parseFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var currentSection string

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "  "))
		if strings.HasSuffix(line, ":") && !strings.Contains(trimmed, ":") {
			continue
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "server":
			currentSection = "server"
		case "llama_swap":
			currentSection = "llama_swap"
		case "logging":
			currentSection = "logging"
		case "listen":
			if currentSection == "server" {
				cfg.Server.Listen = unquote(val)
			}
		case "url":
			if currentSection == "llama_swap" {
				cfg.LlamaSwap.URL = unquote(val)
			}
		case "level":
			if currentSection == "logging" {
				cfg.Logging.Level = unquote(val)
			}
		case "metrics":
			currentSection = "metrics"
		case "ttl":
			if currentSection == "metrics" {
				if d, err := time.ParseDuration(val); err == nil {
					cfg.MetricsTTL = d
				}
			}
		case "active_ttl":
			if currentSection == "metrics" {
				if d, err := time.ParseDuration(val); err == nil {
					cfg.MetricsActiveTTL = d
				}
			}
		}
	}

	return scanner.Err()
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			unq, err := strconv.Unquote(s)
			if err == nil {
				return unq
			}
		}
	}
	return s
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SERVER_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("LLAMA_SWAP_URL"); v != "" {
		cfg.LlamaSwap.URL = v
	}
	if v := os.Getenv("LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("METRICS_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MetricsTTL = d
		}
	}
	if v := os.Getenv("METRICS_ACTIVE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MetricsActiveTTL = d
		}
	}
}
