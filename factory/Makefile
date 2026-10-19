# 厂内工程常用动作。开发机：make test / make dev-up（按源码重建）；部署机只需 docker + make：make up / make upgrade（不重建 app，版本只经 OTA 更换）。
SHELL := /bin/bash
COMPOSE ?= docker compose
# 打进二进制的构建号（探活 build）；镜像标签另由 .env 的 WMESH_VERSION 决定，默认 dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
export WMESH_BUILD_VERSION := $(VERSION)
# 离线包目标平台，默认 x86 厂机；arm64 厂机用 make bundle BUNDLE_PLATFORM=linux/arm64
BUNDLE_PLATFORM ?= linux/amd64
# 包名末尾的架构：去掉 linux/、其余斜杠换横杠，如 amd64
BUNDLE_ARCH = $(subst /,-,$(patsubst linux/%,%,$(BUNDLE_PLATFORM)))
# docker save --platform 要 Docker 28+；旧版经典存储一个标签只存一种架构，不带也只导出上面拉好的目标架构
BUNDLE_SAVE_FLAGS = $(if $(shell docker save --help 2>/dev/null | grep -- --platform),--platform $(BUNDLE_PLATFORM))

.DEFAULT_GOAL := help
.PHONY: help env test-db test-db-down test guard migrations-sum build up dev-up down logs ps upgrade bundle push dev-server dev-web

help: ## 列出可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

env: ## 没有 .env 时从 .env.example 生成（生产务必改密码）
	@test -f .env || (cp .env.example .env && echo ">> created .env from .env.example; edit secrets before production")

test-db: ## 起 go test 用的一次性测试库（:55433）
	cd server && $(COMPOSE) up -d --wait

test-db-down: ## 停掉测试库
	cd server && $(COMPOSE) down

guard: ## 注释与分层门禁（对齐 HTGlobal）
	python3 ../scripts/style_guard.py

migrations-sum: ## 新增迁移后登记进 server/migrations/migrations.sum（只追加新文件，改了已登记的迁移照样报错）
	cd server && WMESH_UPDATE_MIGRATIONS_SUM=1 go test -count=1 ./migrations/

test: test-db ## 门禁 + 服务端 vet + 矩阵验收，前端 lint + 类型检查
	python3 ../scripts/style_guard.py
	cd server && go vet ./... && go test ./...
	cd frontend && npm ci --no-audit --no-fund && npm run lint && npm run typecheck

build: env ## 构建应用镜像（二进制构建号取 git describe，镜像标签取 .env 的 WMESH_VERSION）
	$(COMPOSE) build --pull

up: env ## 拉起 app + db + oss + alloy 并等到健康，再重建 updater 换上最新 apply.sh；沿用本机已有 app 镜像（OTA 换上或 docker load），本机没有才构建
	$(COMPOSE) up -d --wait
	$(COMPOSE) up -d --no-deps --force-recreate updater

dev-up: env ## 开发机按当前源码重建 app 并拉起、等到健康，再重建 updater（会覆盖本机同标签 app 镜像，部署机别用）
	$(COMPOSE) up -d --build --wait
	$(COMPOSE) up -d --no-deps --force-recreate updater

down: ## 停止并移除容器，保留数据卷
	$(COMPOSE) down

logs: ## 跟随应用日志
	$(COMPOSE) logs -f app

ps: ## 查看容器状态
	$(COMPOSE) ps

upgrade: env ## 升级：拉新基础镜像、替换容器并重建 updater，不重建 app（版本只经 OTA 更换）；各厂库迁移在首次访问时自动向前执行
	$(COMPOSE) pull --ignore-buildable --ignore-pull-failures
	$(COMPOSE) up -d --wait --remove-orphans
	$(COMPOSE) up -d --no-deps --force-recreate updater

bundle: env ## 按 BUNDLE_PLATFORM（默认 linux/amd64）导出离线安装包 dist/*.tar（含 app/db/oss/alloy 镜像），目标机 docker load -i 后 make up；打完本机这些镜像是目标架构，本机再跑先 make dev-up 换回；Linux 非 amd64 打包机先装 binfmt
	DOCKER_DEFAULT_PLATFORM=$(BUNDLE_PLATFORM) $(COMPOSE) build --pull
	DOCKER_DEFAULT_PLATFORM=$(BUNDLE_PLATFORM) $(COMPOSE) pull --ignore-buildable --ignore-pull-failures
	mkdir -p dist && docker save $(BUNDLE_SAVE_FLAGS) -o dist/wmesh-factory-images-$(VERSION)-$(BUNDLE_ARCH).tar $$($(COMPOSE) config --images)
	@ls -lh dist/wmesh-factory-images-$(VERSION)-$(BUNDLE_ARCH).tar
	@echo ">> 包内镜像为 $(BUNDLE_PLATFORM)；本机 app 等同标签镜像也换成了该架构，本机要再跑先 make dev-up 换回本机架构"
	@echo ">> Linux 打包机与目标架构不同（如 arm64 机打 amd64）要先装 binfmt 才能构建"

push: build ## 推送应用镜像到 .env 里的 WMESH_REGISTRY
	$(COMPOSE) push app

dev-server: test-db ## 本地跑 Go 服务（连测试库；改 WMESH_* 环境变量覆盖默认值）
	cd server && WMESH_BOOTSTRAP_TOKEN=$${WMESH_BOOTSTRAP_TOKEN:-dev-bootstrap} go run ./cmd/server

dev-web: ## 本地跑前端开发服务器（:5174，/v1 代理到 :8081）
	cd frontend && npm install --no-audit --no-fund && npm run dev
