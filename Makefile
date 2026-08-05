.PHONY: api scheduler pusher build tidy \
	up down start stop restart status logs ps rebuild rebuild-api rebuild-web down-v help

GO ?= go
CFG ?= configs/config.yaml
COMPOSE ?= docker compose

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

# ---- Docker 一键管理 ----
# 用法：
#   make up / make start      启动（含构建）
#   make down / make stop     停止并移除容器（保留数据卷）
#   make restart              重启全部服务
#   make status / make ps     查看状态
#   make logs                 跟踪核心服务日志
#   make rebuild              强制重建并启动
#   make down-v               停止并删除数据卷（危险）

help:
	@echo "Starlink Docker 常用命令："
	@echo "  make up | start      启动全栈（build + up -d）"
	@echo "  make down | stop     停止并移除容器（保留 MySQL/Redis 数据）"
	@echo "  make restart         重启全部服务"
	@echo "  make status | ps     查看容器状态"
	@echo "  make logs            跟踪 api/scheduler/pusher/web 日志"
	@echo "  make rebuild         强制重建镜像并启动"
	@echo "  make rebuild-api     仅重建后端 api 镜像"
	@echo "  make rebuild-web     仅重建前端 web 镜像"
	@echo "  make down-v          停止并删除数据卷（不可恢复）"

up start:
	$(COMPOSE) up -d --build
	@echo ""
	@echo "已启动："
	@echo "  Web  http://localhost:3000"
	@echo "  API  http://localhost:8080/healthz"
	@$(COMPOSE) ps

down stop:
	$(COMPOSE) down
	@echo "已停止（数据卷保留）"

restart:
	$(COMPOSE) restart
	@$(COMPOSE) ps

status ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs -f api scheduler pusher web

rebuild:
	$(COMPOSE) up -d --build --force-recreate
	@$(COMPOSE) ps

rebuild-api:
	$(COMPOSE) build api
	$(COMPOSE) up -d --force-recreate api scheduler pusher
	@$(COMPOSE) ps

rebuild-web:
	$(COMPOSE) build web
	$(COMPOSE) up -d --force-recreate web
	@$(COMPOSE) ps

down-v:
	$(COMPOSE) down -v
	@echo "已停止并删除数据卷"

# 兼容旧目标名
docker-up: up
docker-down: down
docker-logs: logs
docker-rebuild: rebuild
