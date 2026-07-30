// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package llama

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LogStream interface {
	Start() error
	Stop()
	Subscribe(func(string))
}

type ConnectedChecker interface {
	Connected() bool
}

type Waiter interface {
	Wait()
}

type llamaStream struct {
	mu         sync.RWMutex
	url        string
	client     *http.Client
	hcClient   *http.Client
	ctx        context.CancelFunc
	hcCancel   context.CancelFunc
	subs       []func(string)
	connected  bool
	curBody    io.ReadCloser
	log        *slog.Logger
	wg         sync.WaitGroup
}

func NewLogStream(baseURL string, log *slog.Logger) LogStream {
	transport := &http.Transport{
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 2,
	}
	sseClient := &http.Client{
		Transport: transport,
	}
	hcClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
	return &llamaStream{
		url:      baseURL,
		client:   sseClient,
		hcClient: hcClient,
		log:      log,
	}
}

func (s *llamaStream) Subscribe(fn func(string)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

func (s *llamaStream) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.ctx = cancel
	s.mu.Unlock()

	s.wg.Add(2)
	s.startHealthCheck(ctx)
	go s.run(ctx)

	return nil
}

func (s *llamaStream) Stop() {
	s.mu.Lock()
	if s.ctx != nil {
		s.ctx()
	}
	if s.hcCancel != nil {
		s.hcCancel()
	}
	s.mu.Unlock()
}

func (s *llamaStream) Wait() {
	s.wg.Wait()
}

func (s *llamaStream) broadcast(line string) {
	s.mu.RLock()
	subs := make([]func(string), len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()

	for _, fn := range subs {
		fn(line)
	}
}

func (s *llamaStream) setConnected(v bool) {
	s.mu.Lock()
	s.connected = v
	s.mu.Unlock()
}

func (s *llamaStream) setCurBody(body io.ReadCloser) {
	s.mu.Lock()
	s.curBody = body
	s.mu.Unlock()
}

func (s *llamaStream) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

func (s *llamaStream) startHealthCheck(parentCtx context.Context) {
	_, hcCancel := context.WithCancel(parentCtx)
	s.mu.Lock()
	s.hcCancel = hcCancel
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-parentCtx.Done():
				return
			case <-ticker.C:
				s.checkHealth()
			}
		}
	}()
}

func (s *llamaStream) checkHealth() {
	s.mu.RLock()
	connected := s.connected
	curBody := s.curBody
	s.mu.RUnlock()
	if !connected || curBody == nil {
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", s.url+"/health", nil)
	if err != nil {
		return
	}

	resp, err := s.hcClient.Do(req)
	if err != nil {
		s.log.Warn("health-check failed, forcing reconnect", "err", err)
		s.setConnected(false)
		curBody.Close()
		return
	}
	resp.Body.Close()
}

func (s *llamaStream) run(ctx context.Context) {
	defer s.wg.Done()
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			s.setConnected(false)
			return
		default:
		}

		err := s.connectAndRead(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("stream error, reconnecting", "err", err, "backoff", backoff)
		}

		s.setConnected(false)

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

func (s *llamaStream) connectAndRead(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.url+"/logs/stream/upstream", nil)
	if err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	s.setConnected(true)
	s.setCurBody(resp.Body)
	s.log.Info("connected to llama-swap stream")

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

		s.broadcast(line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return fmt.Errorf("stream closed by server")
}
