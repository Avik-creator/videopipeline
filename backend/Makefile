.PHONY: run build tidy deps check-ffmpeg

# ── Configuration ─────────────────────────────────────────────────────────────
BINARY  = videopipe
SERVER  = ./cmd/server
DATA    = ./videos
DB      = ./videopipe.db
ADDR    = :8080
WORKERS = 4

# ── Commands ──────────────────────────────────────────────────────────────────

## Install dependencies
deps:
	go mod tidy
	go mod download

## Check ffmpeg is installed
check-ffmpeg:
	@which ffmpeg > /dev/null 2>&1 || (echo "❌ ffmpeg not found. Install it: brew install ffmpeg  OR  apt install ffmpeg" && exit 1)
	@which ffprobe > /dev/null 2>&1 || (echo "❌ ffprobe not found. It ships with ffmpeg." && exit 1)
	@echo "✅ ffmpeg and ffprobe found"

## Build binary
build: deps
	CGO_ENABLED=1 go build -o $(BINARY) $(SERVER)
	@echo "✅ Built ./$(BINARY)"

## Run in development mode (no binary)
run: check-ffmpeg deps
	CGO_ENABLED=1 go run $(SERVER) \
		-addr $(ADDR) \
		-data $(DATA) \
		-db   $(DB) \
		-workers $(WORKERS)

## Build and run binary
start: build check-ffmpeg
	./$(BINARY) -addr $(ADDR) -data $(DATA) -db $(DB) -workers $(WORKERS)

## Run with race detector (for development)
dev: check-ffmpeg deps
	CGO_ENABLED=1 go run -race $(SERVER) \
		-addr $(ADDR) -data $(DATA) -db $(DB) -workers 2

## Clean build artifacts and data
clean:
	rm -f $(BINARY) $(DB)
	rm -rf $(DATA)/raw $(DATA)/hls $(DATA)/thumbs

## Lint
lint:
	golangci-lint run ./...

## Show help
help:
	@grep -E '^## ' Makefile | sed 's/## //'
