PORT ?= $(shell grep -E '^SERVER_PORT=' .env 2>/dev/null | cut -d= -f2)
URL ?= http://localhost:$(or $(PORT),8000)
Q ?= Which driver age band has the highest claim frequency, and how does our claims cost per policy compare with the Irish market?

.PHONY: env up down build run

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
