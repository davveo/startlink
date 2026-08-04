.PHONY: api scheduler pusher build tidy docker-up docker-down docker-logs docker-rebuild

GO ?= go
CFG ?= configs/config.yaml

tidy:
	$(GO) mod tidy

build:
	$(GO) build -o bin/api ./cmd/api
	$(GO) build -o bin/scheduler ./cmd/scheduler
	$(GO) build -o bin/pusher ./cmd/pusher

api:
	$(GO) run ./cmd/api -config $(CFG)

scheduler:
	$(GO) run ./cmd/scheduler -config $(CFG)

pusher:
	$(GO) run ./cmd/pusher -config $(CFG)

# ---- Docker 一键启停 ----
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f api scheduler pusher

docker-rebuild:
	docker compose up -d --build --force-recreate
