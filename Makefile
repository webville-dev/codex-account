MODULE := nyashachiroro.com/codex-account
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X $(MODULE)/internal/version.Commit=$(COMMIT) -X $(MODULE)/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: fmt vet test race build check snapshot release

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

check:
	go mod verify
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...
	go test -race ./...
	go build ./...

snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean
