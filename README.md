<!-- Copyright (c) 2026 Bohdan Futerko
Website: https://www.bf.com.ua
GitHub: https://github.com/LeBohdan -->

# llama-swap-pulse

Version: 1.3

A standalone telemetry bridge between `llama-swap` and external clients such as OpenCode plugins.

## Overview

The service connects to `llama-swap`'s live upstream logs, parses `llama.cpp` runtime timing information, converts raw log lines into structured real-time metrics, and exposes clean APIs for consumers.

## Features

- Connects to `llama-swap` log stream (`/logs/stream/upstream`)
- Parses llama.cpp timing messages in real-time
- Maintains current inference state (active tasks, progress, TPS)
- Provides real-time SSE streaming metrics (`/pulse/live`)
- Provides current snapshot API (`/pulse/metrics`)
- Automatic reconnection with exponential backoff (1s → 30s max)
- Survives llama-swap restarts
- Single binary, standard library only

## Usage

```bash
LLAMA_SWAP_URL=http://localhost:8080 SERVER_LISTEN=:8090 ./dist/llama-swap-pulse
```

Or with a config file:

```bash
./dist/llama-swap-pulse -config config.yaml
```

### Configuration

Configuration is loaded from a YAML file and can be overridden by environment variables.

```yaml
# config.yaml
server:
  listen: ":8090"

llama_swap:
  url: "http://localhost:8080"

logging:
  level: info
```

Environment variable overrides:

| Variable        | Description       | Default               |
|-----------------|-------------------|-----------------------|
| `SERVER_LISTEN` | HTTP listen addr  | `:8090`               |
| `LLAMA_SWAP_URL`| llama-swap base URL| `http://localhost:8080`|
| `LOGGING_LEVEL` | Log level         | `info`                |

## API

All endpoints are prefixed with `/pulse`.

For full API documentation — endpoint schemas, event types, SSE behavior, task lifecycle, and CORS — see [docs/api.md](docs/api.md).

Quick reference:

| Endpoint | Description |
|----------|-------------|
| `GET /pulse/health` | Health check (`200` connected, `503` disconnected) |
| `GET /pulse/metrics` | Current task snapshot (JSON) |
| `GET /pulse/live` | Real-time SSE event stream |

## Building

```bash
go build -o dist/llama-swap-pulse ./cmd
```

## Installation as a Service

### Build and install binary

```bash
go build -o dist/llama-swap-pulse ./cmd
sudo install -m 755 dist/llama-swap-pulse /usr/local/bin/llama-swap-pulse
```

### Create config directory

```bash
sudo mkdir -p /etc/llama-swap-pulse
# Copy your config file there, or rely on Environment vars in the unit file
```

### Install and enable systemd service

```bash
sudo cp deploy/llama-swap-pulse.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable llama-swap-pulse
sudo systemctl start llama-swap-pulse
```

### Verify

```bash
sudo systemctl status llama-swap-pulse
sudo journalctl -u llama-swap-pulse -f
```

The unit file (`deploy/llama-swap-pulse.service`) is pre-configured with:
- `Restart=on-failure` (5s delay)
- All config as `Environment` variables
- Binary path: `/usr/local/bin/llama-swap-pulse`
- Config path: `/etc/llama-swap-pulse/config`
- Logs to `journalctl`

## License

This service does NOT modify llama-swap or llama.cpp. It is a standalone read-only telemetry bridge.
