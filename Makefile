PORT ?= $(shell grep -E '^SERVER_PORT=' .env 2>/dev/null | cut -d= -f2)
URL ?= http://localhost:$(or $(PORT),8000)
Q ?= Which driver age band has the highest claim frequency, and how does our claims cost per policy compare with the Irish market?

.PHONY: env up down build run mcp import search ask eval lint

env:
	./scripts/env.sh

up:
	docker compose up -d

down:
	docker compose down

build:
	go build -o bin/golabagent .

run:
	go run .

mcp: build
	./bin/golabagent -mcp

import:
	curl -sS -X POST $(URL)/import | jq

search:
	curl -sS -G $(URL)/search --data-urlencode "q=$(Q)" | jq '.hits[] | {source, score, vector_rank, keyword_rank}'

ask:
	curl -sS -X POST $(URL)/ask -H 'Content-Type: application/json' \
		-d "$$(jq -n --arg q "$(Q)" '{question: $$q}')" | jq

eval:
	GOLAB_URL=$(URL) go test -tags=eval -v -count=1 -timeout 60m ./evals

lint:
	go vet ./...
	go vet -tags=eval ./evals
