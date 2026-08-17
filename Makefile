MODULE := nyashachiroro.com/codex-account
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT) -X $(MODULE)/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: fmt vet test race build release-linux

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

build:
	go build -ldflags "$(LDFLAGS)" -o bin/codex-account ./cmd/codex-account

release-linux:
	@test "$(VERSION)" != "dev" || (echo "VERSION is required (for example: make release-linux VERSION=v1.2.3)" >&2; exit 2)
	./scripts/release-linux.sh "$(VERSION)"
