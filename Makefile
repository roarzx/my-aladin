SHELL := /bin/sh

GO ?= go
BIN := bin

COMPOSE ?= docker compose -f deploy/docker-compose.yml
DSN ?= root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4
REDIS ?= 127.0.0.1:6379

.PHONY: all build test lint fmt integrate verify up down reset migrate test-integration test-e2e perf-test web-test clean

all: build

build:
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/server ./cmd/server
	$(GO) build -o $(BIN)/daemon ./cmd/daemon
	$(GO) build -o $(BIN)/aladdin ./cmd/aladdin

test:
	$(GO) test ./... -count=1

lint:
	$(GO) vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint 未安装，跳过"

fmt:
	gofmt -l -w .

# ---- 本地依赖（Docker） ----

up:
	$(COMPOSE) up -d --wait

down:
	$(COMPOSE) down

# 清空本地数据卷后重建（会丢数据）
reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait
	$(MAKE) migrate

# ---- 数据库与测试 ----

migrate:
	$(GO) run ./cmd/aladdin migrate --dsn '$(DSN)'

test-integration:
	TEST_MYSQL_DSN='$(DSN)' TEST_REDIS_ADDR='$(REDIS)' \
		$(GO) test -tags=integration ./... -count=1 -p 1

# 本地一键验证：起依赖 → 迁移 → 集成测试 → 单测
verify: up migrate test-integration test

test-e2e:
	TEST_MYSQL_DSN='$(DSN)' TEST_REDIS_ADDR='$(REDIS)' \
		$(GO) test -tags=e2e ./tests/e2e/... -count=1

perf-test:
	TEST_MYSQL_DSN='$(DSN)' TEST_REDIS_ADDR='$(REDIS)' PERF_N=$(or $(PERF_N),30) \
		$(GO) test -tags=perf ./tests/perf/... -count=1 -v

web-test:
	@if [ -d web ]; then pnpm --filter web test && pnpm --filter web build; else echo "web/ 尚未创建，跳过"; fi

clean:
	rm -rf $(BIN)
