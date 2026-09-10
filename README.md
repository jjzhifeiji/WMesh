# WMesh

焊接机器人场景的 **WAN + Factory + Client** 分层平台。需求基线见 `docs/平台架构核心需求.md`，推进计划见 `docs/总体计划.md`。

仓库里是两个彼此独立、可单独构建部署的工程：

| 目录 | 是什么 | 说明 |
| --- | --- | --- |
| [`global/`](global/README.md) | WAN 跨厂总控 | 唯一 WAN 管理员、工厂名录、给每个厂下发一名初始超管；不碰厂内人员；独立对象存储放平台级资产 |
| [`factory/`](factory/README.md) | 厂内服务 | 本厂人员账号、自定义组织树、角色作用域、会话与审计；每厂一个独立库 + 本地对象存储 |
| `docs/` | 需求、规格、验收矩阵 | 业务对错以此为准 |

每个工程目录下只有 `frontend/`（React + Ant Design 管理后台）与 `server/`（Go），外加自己的 `Dockerfile`、`docker-compose.yml`、`Makefile`、`README.md`。两侧 Go 模块互不导入，WAN 库里没有厂内表，两侧对象存储互不相通。

## 快速开始

```bash
# 厂内（app + PostgreSQL 18 + RustFS 对象存储）
cd factory && make up

# WAN（app + PostgreSQL 18 + RustFS 对象存储）
cd global && make up
```

首次 `make up` 会把 `.env.example` 复制成 `.env`；生产环境先改掉里面的口令，并保证两侧 `WMESH_BOOTSTRAP_TOKEN` 一致。

- WAN 管理端：http://localhost:8080 　厂内管理端：http://localhost:8081
- 对象存储管理台：WAN http://localhost:9001 　厂内 http://localhost:9003（用 `.env` 里的 OSS 密钥登录）
- 探活：`/healthz`（返回版本、库与 OSS 状态）
- 常用命令：`make help`

## 开发与验证

```bash
cd factory && make test     # 起测试库 → go vet + 矩阵验收 → 前端 oxlint + 类型检查
cd global  && make test
```

工具链：Go 1.27、Node 24（各 `frontend/` 下有 `.node-version`）、Docker Compose v2。
