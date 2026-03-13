# Pikostack

Single-binary VPS service management. Run everything; Docker containers, raw processes, systemd units, and URL watchdogs; from one place, with a TUI and a full web dashboard (Pikoview).

```
pikostack serve      # Start Pikoview web dashboard + monitor engine
pikostack tui        # Launch interactive terminal UI
```

---

## Features

- **Dashboard**: live service grid, event feed, host CPU/MEM stats
- **Services**: deploy, start, stop, restart any service type
- **Auto-restart**: background goroutines watch health and restart on failure (configurable max attempts)
- **Live logs**: WebSocket log streaming per service
- **Analytics**: Chart.js CPU/memory charts, service uptime, status distribution
- **TUI**: full Bubble Tea terminal interface with the same controls as the web UI
- **Single binary**: templates and static assets embedded with `//go:embed`
- **Config file**: `pikostack.yaml` with `PIKO_` env var overrides

---

## Service Types

| Type | Description |
|------|-------------|
| `docker` | Docker container (run/start/stop/restart) |
| `compose` | docker-compose project |
| `process` | Raw OS process (command + working dir) |
| `systemd` | Existing systemd unit |
| `url` | Watchdog only; monitors a URL, no restart |

---

## Quick Start

```bash
# Build
make build

# Run (default: http://0.0.0.0:7331)
./pikostack serve

# Or with custom port
PIKO_SERVER_PORT=8080 ./pikostack serve

# TUI
./pikostack tui

# With config file
./pikostack serve --config /etc/pikostack/pikostack.yaml
```

---

## Configuration

```yaml
# pikostack.yaml
server:
  host: 0.0.0.0
  port: 7331
  secret: change-me

auth:
  enabled: false
  username: admin
  password: pikostack

database:
  path: ./pikostack.db

monitor:
  interval: 15s
  grace_period: 30s
  max_restarts: 5
  metrics_retention_days: 7
  events_retention_days: 30
```

All keys can be overridden with `PIKO_` prefixed env vars:

```text
PIKO_SERVER_PORT=8080
PIKO_AUTH_ENABLED=true
PIKO_AUTH_PASSWORD=secret
PIKO_MONITOR_INTERVAL=30s
```

---

## API

REST base: `http://localhost:7331/api/v1`

```text
GET    /api/v1/services
POST   /api/v1/services
GET    /api/v1/services/:id
PUT    /api/v1/services/:id
DELETE /api/v1/services/:id
POST   /api/v1/services/:id/start
POST   /api/v1/services/:id/stop
POST   /api/v1/services/:id/restart
GET    /api/v1/services/:id/events
GET    /api/v1/services/:id/metrics

GET    /api/v1/projects
POST   /api/v1/projects
GET    /api/v1/projects/:id
DELETE /api/v1/projects/:id

GET    /api/v1/analytics/overview
GET    /api/v1/analytics/system
GET    /api/v1/system/stats
GET    /api/v1/events

WS     /ws/events          # global event broadcast
WS     /ws/logs/:id        # live log stream
```

---

## Stack

- **Go**: single binary, CGO for SQLite
- **Gin**: HTTP API
- **gorilla/websocket**: log streaming + event broadcast
- **GORM + SQLite**: service/event/metrics persistence
- **Viper + Cobra**: config + CLI
- **Bubble Tea + Lip Gloss**: TUI
- **HTMX + Alpine.js + Tailwind**: web UI (all CDN, no build step)
- **Chart.js**: analytics charts

---

## Install as systemd service

```bash
make install
make systemd-unit | sudo tee /etc/systemd/system/pikostack.service
sudo systemctl daemon-reload
sudo systemctl enable --now pikostack
```
