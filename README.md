# WMesh

焊接机器人场景的 **WAN + Factory + Client** 分层平台。需求基线见 `docs/平台架构核心需求.md`（已锁定），推进计划见 `docs/总体计划.md`。

仓库里是彼此独立、可单独构建部署的工程：

| 目录 | 是什么 | 说明 |
| --- | --- | --- |
| [`global/`](global/README.md) | WAN 跨厂总控 | 唯一 WAN 管理员、工厂名录、给每个厂下发一名初始超管；不碰厂内人员；独立对象存储放平台级资产 |
| [`factory/`](factory/README.md) | 厂内服务 | 本厂人员账号、自定义组织树、角色作用域、会话与审计；每厂一个独立库 + 本地对象存储 |
| [`client/`](client/README.md) | 现场示教器 | Android 单模块；包作用域见该 README |
| `docs/` | 需求、规格、验收矩阵 | 业务对错以此为准 |

`global/` 与 `factory/` 各自只有 `frontend/`（React）和 `server/`（Go），外加自己的部署文件。两侧 Go 模块互不导入，WAN 库里没有厂内表，两侧对象存储互不相通。`client/` 是独立的 Android 工程，包作用域见其 README。

## 快速开始

```bash
# 本机按当前源码重建并拉起（默认云端+厂端；只云端加 wan，只厂端加 factory）
./scripts/dev-up.sh

# 或分工程：
cd factory && make up    # 厂内 http://localhost:52081
cd global && make up     # WAN  http://localhost:52080
```

首次 `make up` 会把 `.env.example` 复制成 `.env`；生产环境先改掉里面的密码，并保证两侧 `WMESH_BOOTSTRAP_TOKEN` 一致。

- WAN 管理端：http://localhost:52080 　厂内管理端：http://localhost:52081
- 对象存储管理台：WAN http://localhost:52901 　厂内 http://localhost:52903（用 `.env` 里的 OSS 密钥登录）
- 探活：`/healthz`（返回版本、库与 OSS 状态）
- 常用命令：`make help`

## 开发与验证

```bash
cd factory && make test     # 起测试库 → go vet + 矩阵验收 → 前端 oxlint + 类型检查
cd global  && make test
```

工具链：Go 1.27、Node 24（各 `frontend/` 下有 `.node-version`）、Docker Compose v2。
