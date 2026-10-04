.PHONY: build test race vet lint fmt cover skill skill-check

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/snow ./cmd/snow

test:
	go test ./...

race:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...

# Coverage gate: domain and use-case packages must stay at or above 90%.
cover:
	go test -cover ./internal/domain/... ./internal/usecase/...

# Generates the agent skill document (command owned by WS-E, task E4).
skill:
	go run ./cmd/snow skill generate

# Fails when the generated skill document drifts from the committed golden.
skill-check:
	go run ./cmd/snow skill generate --out dist/snow-cli.md && diff -u internal/cli/testdata/skill.golden.md dist/snow-cli.md
