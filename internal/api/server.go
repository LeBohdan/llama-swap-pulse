// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"llama-swap-pulse/internal/llama"
	"llama-swap-pulse/internal/metrics"
	"llama-swap-pulse/internal/models"
)

type Server struct {
	addr     string
	store    *metrics.Store
	stream   llama.LogStream
	connected llama.ConnectedChecker
	log      *slog.Logger
	mu       sync.Mutex
	srv      *http.Server
	errCh    chan error
}

func New(addr string, store *metrics.Store, stream llama.LogStream, connected llama.ConnectedChecker, log *slog.Logger) *Server {
	return &Server{
		addr:      addr,
		store:     store,
		stream:    stream,
		connected: connected,
		log:       log,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /pulse/health", s.handleHealth)
	mux.HandleFunc("GET /pulse/metrics", s.handleMetrics)
	mux.HandleFunc("GET /pulse/live", s.handleLive)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	s.mu.Lock()
	s.srv = &http.Server{Addr: s.addr, Handler: mux}
	s.errCh = make(chan error, 1)
	s.mu.Unlock()

	s.log.Info("api server listening", "addr", s.addr)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.errCh <- err
		}
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) ErrCh() <-chan error {
	return s.errCh
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if !s.connected.Connected() {
		status = http.StatusServiceUnavailable
	}
	resp := map[string]any{
		"status":              "ok",
		"llama_swap_connected": s.connected.Connected(),
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snapshot := s.store.CurrentSnapshot()
	json.NewEncoder(w).Encode(snapshot)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Cache-Control")

	flusher.Flush()

	snapshot := s.store.CurrentSnapshot()
	writeSnapshot(w, flusher, snapshot)

	ch := s.store.Subscribe()
	go func() {
		<-r.Context().Done()
		s.store.Unsubscribe(ch)
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, flusher, ev)
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev *models.MetricEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	fmt.Fprintf(w, "event: %s\n", ev.Type)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func writeSnapshot(w http.ResponseWriter, flusher http.Flusher, snapshot metrics.Snapshot) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}

	fmt.Fprintf(w, "event: snapshot\n")
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
