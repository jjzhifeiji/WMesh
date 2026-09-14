# WAN 工程常用动作。开发机：make test；部署机只需 docker + make：make up / make upgrade。
SHELL := /bin/bash
COMPOSE ?= docker compose
# 打进二进制的构建号（探活里可见）；镜像标签另由 .env 的 WMESH_VERSION 决定，默认 dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
export WMESH_BUILD_VERSION := $(VERSION)

.DEFAULT_GOAL := help
.PHONY: help env test-db test-db-down test guard build up down logs ps upgrade bundle push dev-server dev-web

help: ## 列出可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

env: ## 没有 .env 时从 .env.example 生成（生产务必改密码）
	@test -f .env || (cp .env.example .env && echo ">> created .env from .env.example; edit secrets before production")

test-db: ## 起 go test 用的一次性测试库（:55432）
	cd server && $(COMPOSE) up -d --wait

test-db-down: ## 停掉测试库
	cd server && $(COMPOSE) down

guard: ## 注释与分层门禁（对齐 HTGlobal）
	python3 ../scripts/style_guard.py

test: test-db ## 门禁 + 服务端 vet + 矩阵验收，前端 lint + 类型检查
	python3 ../scripts/style_guard.py
	cd server && go vet ./... && go test ./...
	cd frontend && npm ci --no-audit --no-fund && npm run lint && npm run typecheck

build: env ## 构建应用镜像（二进制构建号取 git describe，镜像标签取 .env 的 WMESH_VERSION）
	$(COMPOSE) build --pull

up: env ## 拉起 app + db 并等到健康
	$(COMPOSE) up -d --build --wait

down: ## 停止并移除容器，保留数据卷
	$(COMPOSE) down

logs: ## 跟随应用日志
	$(COMPOSE) logs -f app

ps: ## 查看容器状态
	$(COMPOSE) ps

upgrade: env ## 升级：拉新基础镜像、重建应用、替换容器；库迁移在应用启动时自动向前执行
	$(COMPOSE) pull --ignore-buildable --ignore-pull-failures
	$(COMPOSE) build --pull
	$(COMPOSE) up -d --wait --remove-orphans

bundle: build ## 导出离线安装包 dist/*.tar（含全部镜像），目标机 docker load -i 后 make up
	$(COMPOSE) pull --ignore-buildable --ignore-pull-failures
	mkdir -p dist && docker save -o dist/wmesh-global-images-$(VERSION).tar $$($(COMPOSE) config --images)
	@ls -lh dist/wmesh-global-images-$(VERSION).tar

push: build ## 推送应用镜像到 .env 里的 WMESH_REGISTRY
	$(COMPOSE) push app

dev-server: test-db ## 本地跑 Go 服务（连测试库；改 WMESH_* 环境变量覆盖默认值）
	cd server && WMESH_BOOTSTRAP_TOKEN=$${WMESH_BOOTSTRAP_TOKEN:-dev-bootstrap} \
		WMESH_ADMIN_LOGIN=$${WMESH_ADMIN_LOGIN:-wan} WMESH_ADMIN_PASSWORD=$${WMESH_ADMIN_PASSWORD:-wan-dev-pass} \
		go run ./cmd/server

dev-web: ## 本地跑前端开发服务器（:5173，/v1 代理到 :8080）
	cd frontend && npm install --no-audit --no-fund && npm run dev
