.PHONY: api scheduler pusher build tidy \
	up down start stop restart status logs ps rebuild rebuild-api rebuild-web web-dist down-v help

GO ?= go
CFG ?= configs/config.yaml
COMPOSE ?= docker compose
WEB_DIR := web

# 应用服务：重启/重建只动这些，不动 mysql/redis（避免中断数据与连接）
APP_SERVICES := api scheduler pusher web

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
#   make up / make start      启动全栈（含 mysql/redis；复用已有镜像）
#   make down / make stop     停止并移除全部容器（保留数据卷）
#   make restart              仅重启应用服务（不含 mysql/redis）
#   make rebuild              仅重建并重启应用服务（不含 mysql/redis）
#   make down-v               停止并删除数据卷（危险）

help:
	@echo "Starlink Docker 常用命令："
	@echo "  make up | start      启动全栈（含 mysql/redis；复用本地镜像）"
	@echo "  make down | stop     停止并移除全部容器（保留 MySQL/Redis 数据卷）"
	@echo "  make restart         仅重启应用：$(APP_SERVICES)（不动 mysql/redis）"
	@echo "  make status | ps     查看容器状态"
	@echo "  make logs            跟踪应用日志：$(APP_SERVICES)"
	@echo "  make rebuild         仅重建并重启应用（不动 mysql/redis）"
	@echo "  make rebuild-api     仅重建后端镜像并重启 api/scheduler/pusher"
	@echo "  make rebuild-web     宿主机 npm build + 重建 web 镜像并重启"
	@echo "  make down-v          停止并删除数据卷（不可恢复）"

up start:
	@if ! docker image inspect starlink:latest >/dev/null 2>&1 || \
	   ! docker image inspect starlink-web:latest >/dev/null 2>&1; then \
		echo "本地缺少镜像，执行 make rebuild..."; \
		$(MAKE) rebuild; \
	else \
		$(COMPOSE) up -d; \
	fi
	@echo ""
	@echo "已启动："
	@echo "  Web  http://localhost:3000"
	@echo "  API  http://localhost:8080/healthz"
	@$(COMPOSE) ps

down stop:
	$(COMPOSE) down
	@echo "已停止（数据卷保留）"

restart:
	$(COMPOSE) restart $(APP_SERVICES)
	@$(COMPOSE) ps

status ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs -f $(APP_SERVICES)

# 前端：在宿主机构建 dist，Docker 只打包 alpine+nginx（不拉 node/nginx 官方镜像）
web-dist:
	@command -v npm >/dev/null || (echo "需要本机安装 Node.js/npm"; exit 1)
	cd $(WEB_DIR) && npm ci && npm run build

rebuild:
	$(MAKE) web-dist
	$(COMPOSE) build api web
	$(COMPOSE) up -d --force-recreate $(APP_SERVICES)
	@$(COMPOSE) ps

rebuild-api:
	$(COMPOSE) build api
	$(COMPOSE) up -d --force-recreate api scheduler pusher
	@$(COMPOSE) ps

rebuild-web:
	$(MAKE) web-dist
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
