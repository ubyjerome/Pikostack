BINARY   := pikostack
MODULE   := github.com/pikostack/pikostack
LDFLAGS  := -s -w -X main.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD    := go build -ldflags "$(LDFLAGS)"

.PHONY: all build run run-tui clean deps lint test

all: build

## Download dependencies
deps:
	go mod download
	go mod tidy

## Build single binary
build: deps
	CGO_ENABLED=1 $(BUILD) -o $(BINARY) .

## Build for Linux (static, requires musl)
build-linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(BUILD) -o $(BINARY)-linux-amd64 .

## Run Pikoview web server
run: build
	./$(BINARY) serve

## Run TUI
run-tui: build
	./$(BINARY) tui

## Run with live reload (requires air: go install github.com/air-verse/air@latest)
dev:
	air -- serve

## Clean build artifacts
clean:
	rm -f $(BINARY) $(BINARY)-linux-amd64

## Lint
lint:
	golangci-lint run ./...

## Test
test:
	go test -race ./...

## Print version
version: build
	./$(BINARY) version

## Install to /usr/local/bin
install: build
	install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed: /usr/local/bin/$(BINARY)"

## Generate a systemd unit file for pikostack itself
systemd-unit:
	@cat <<'EOF'
[Unit]
Description=Pikostack service manager
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/pikostack serve
WorkingDirectory=/opt/pikostack
Restart=always
RestartSec=5
Environment=PIKO_SERVER_PORT=7331

[Install]
WantedBy=multi-user.target
EOF
