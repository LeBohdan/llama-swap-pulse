// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"llama-swap-pulse/internal/api"
	"llama-swap-pulse/internal/llama"
	"llama-swap-pulse/internal/metrics"
	"llama-swap-pulse/internal/parser"
	"llama-swap-pulse/internal/config"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Logging.Level)); err != nil {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	log.Info("starting llama-swap-pulse v1.0")
	log.Info("config loaded", "listen", cfg.Server.Listen, "llama_swap_url", cfg.LlamaSwap.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := metrics.NewStore(ctx, cfg.MetricsTTL, cfg.MetricsActiveTTL)
	p := parser.New()

	stream := llama.NewLogStream(cfg.LlamaSwap.URL, log)

	connectedChecker, _ := stream.(llama.ConnectedChecker)

	stream.Subscribe(func(line string) {
		ev, ok := p.ParseLine(line)
		if !ok {
			return
		}
		store.Update(ev)
	})

	if err := stream.Start(); err != nil {
		log.Error("failed to start stream", "err", err)
		os.Exit(1)
	}

	server := api.New(cfg.Server.Listen, store, stream, connectedChecker, log)

	if err := server.Start(); err != nil {
		log.Error("failed to start server", "err", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		log.Info("shutting down...")
	case err := <-server.ErrCh():
		log.Error("server error, shutting down", "err", err)
	}

	stream.Stop()
	if waiter, ok := stream.(llama.Waiter); ok {
		waiter.Wait()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "err", err)
	}

	store.Stop()
	log.Info("stopped")
}
