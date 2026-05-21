APP_NAME := echorift
BACKEND_DIR := backend
WEB_DIR := apps/web
GO ?= go
PNPM ?= corepack pnpm

.PHONY: backend web test tidy migrate run

backend:
	cd $(BACKEND_DIR) && $(GO) build -o ../bin/$(APP_NAME) ./cmd/echorift

run:
	cd $(BACKEND_DIR) && $(GO) run ./cmd/echorift

tidy:
	cd $(BACKEND_DIR) && $(GO) mod tidy

migrate:
	cd $(BACKEND_DIR) && $(GO) run ./cmd/migrate

test:
	cd $(BACKEND_DIR) && $(GO) test ./...

web:
	$(PNPM) --filter @echorift/web build
