.PHONY: dev build test lint

dev:
	@if [ -d web ]; then \
		(cd web && bun run dev) & \
		web_pid=$$!; \
		trap 'kill "$$web_pid" 2>/dev/null || true' INT TERM EXIT; \
		go run ./cmd/asu serve; \
	else go run ./cmd/asu serve; fi

build:
	@if [ -d web ]; then cd web && bun run generate; fi
	go build -o bin/asu ./cmd/asu

test:
	go test ./...
	@if [ -d web ]; then cd web && bun run test; fi

lint:
	golangci-lint run ./...
	@if [ -d web ]; then cd web && bun run lint; fi
