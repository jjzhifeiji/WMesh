# WMesh Global（WAN 跨厂总控）

WAN 侧唯一的人员入口。只做三件事：登录唯一 WAN 管理员、维护工厂名录、发出建厂码供厂端出站认领。厂内人员、组织、角色和日常密码一律不经过这里——WAN 库里根本没有这些表。WAN 有自己独立的对象存储，放平台级资产，与各厂 OSS 互不相通。

## 目录

```
global/
├── Dockerfile            前端构建 → Go 静态编译 → alpine 运行时（单进程：API + 管理端静态页）
├── docker-compose.yml    部署：app + db（PostgreSQL 18）+ oss（RustFS，S3 兼容）
├── Makefile              up / down / upgrade / bundle / test …
├── .env.example          部署配置模板，复制为 .env
├── frontend/             管理后台（见下）
└── server/               Go 1.27 服务
    ├── cmd/server        进程入口：读配置、迁移、引导管理员、保证 OSS 桶、起 HTTP
    ├── internal/httpapi  JSON HTTP 适配，只转调应用服务
    ├── internal/service  应用服务：允许 / 拒绝 / 审计
    ├── internal/store    WAN 库读写（GORM，仅结构体对齐，禁 AutoMigrate）
    ├── internal/web      托管已构建的管理端静态页
    ├── internal/platform 底座：config / oss / id / secret / audit / domain / migrate / testpg
    ├── migrations/       只向前的版本化 SQL，进程启动时自动套用
    └── docker-compose.yml  仅 go test 用的一次性测试库（:55432）
```

### 管理后台（frontend/）

React 19 + TypeScript 7 + Vite 8，UI 用 Ant Design 6，路由 React Router，服务端状态 TanStack Query，lint 用 oxlint。按功能分层，和服务端的 `httpapi → service → store` 一样各管一段：

```
src/
├── main.tsx          挂载：Providers + Router
├── app/              应用装配：providers（antd 中文/主题、QueryClient）、router（路由表）、routes（路径常量）、navigation（菜单）
├── layouts/          页面骨架：AdminLayout（侧栏 + 顶栏 + 内容区）、AuthLayout（登录居中卡片）
├── shared/           跨功能复用，不含业务
│   ├── api/          client（唯一请求出口：带令牌、翻译错误码、401 清会话）、errors（错误码 → 中文）
│   ├── auth/         session（令牌存取，可订阅）、RequireAuth（路由守卫）
│   ├── ui/           PageHeader / SecretOnce / IdText / NotFoundPage 等通用组件
│   └── format.ts     时间、ID 展示
└── features/         每个业务功能一个目录：api.ts（类型 + 查询/变更 hook）+ 页面 + 弹窗
    ├── auth/         登录
    ├── dashboard/    概览（名录规模、服务健康）
    ├── factories/    工厂名录、创建工厂（一次性建厂码）
    ├── clients/      Client 公钥与一机一厂绑定
    └── assets/       平台级工艺与工程、用厂级快照升档
```

加一个功能：在 `features/<name>/` 写 `api.ts` 与页面 → `app/router.tsx` 加路由 → `app/navigation.tsx` 加菜单。页面不直接 `fetch`，一律经 `shared/api/client`。

## 运行

```bash
make up        # 首次自动生成 .env，请改密码；构建镜像并等到 app/db/oss 全部健康
make logs      # 跟随日志（JSON）
make upgrade   # 拉新基础镜像、重建应用、替换容器；库迁移随启动自动向前
make bundle    # 导出离线安装包 dist/*.tar（app + postgres + rustfs），目标机 docker load -i 后 make up
make down      # 停容器，保留数据卷
```

关键配置（`.env`）：

| 变量 | 作用 |
| --- | --- |
| `POSTGRES_PASSWORD` | WAN 库密码 |
| `WMESH_ADMIN_LOGIN` / `WMESH_ADMIN_PASSWORD` | 唯一 WAN 管理员，只在库里没有管理员时写入一次 |
| `WMESH_ADMIN_RESET` | 设为 `true` 时启动按上面密码覆盖已有管理员哈希；用完关掉 |
| `OSS_ACCESS_KEY` / `OSS_SECRET_KEY` / `OSS_BUCKET` | 对象存储密钥与默认桶；桶由 app 启动时自动创建 |
| `OSS_PORT` / `OSS_CONSOLE_PORT` | S3 端点（默认 52900）与 RustFS 管理台（默认 52901，用同一对密钥登录） |
| `WMESH_REGISTRY` / `WMESH_VERSION` | 镜像前缀与标签，供 `make push` |

服务进程直接读 `WMESH_*` 环境变量（见 `server/internal/platform/config`）；`/healthz` 报告版本、库与 OSS 状态。

## 开发

```bash
make test        # 起测试库 → go vet + go test（账号矩阵 WAN 部分 + 节点 0.1～0.4）→ 前端 oxlint + tsc
make dev-server  # 本地跑 Go（连测试库 :55432）
make dev-web     # Vite 开发服务器 :5173，/v1 与 /healthz 代理到 :52080
```

## HTTP

- `GET /healthz` 版本、库、OSS 状态
- `POST /v1/login` `POST /v1/logout` `GET /v1/me`
- `GET /v1/directory` 工厂名录 + 各厂初始超管身份（不含密码）
- `POST /v1/factories` 建厂并返回一次性建厂码（只出现这一次，不落 WAN 库原文）
- `POST /v1/channel/enroll` 用建厂码换待认领身份（不消耗建厂码）
- `POST /v1/channel/claim` 交厂钥并作废建厂码
- `GET /v1/clients` `POST /v1/clients` `POST /v1/clients/{id}/rebind` 节点公钥与一机一厂绑定
- `GET /v1/assets` `POST /v1/assets` 平台级工艺/工程；`assets/{id}/rename|publish`；`POST /v1/assets/promote` 收厂级快照升档
- `GET /v1/templates` `POST /v1/templates` 工艺/工程当前字段模版；保存后下发工厂，不改已有正文；仅新建套用
- 其余 `/v1/factories/{id}/people|orgs|roles|offline-grants|assets`、`/v1/invite-wan-admin` 一律 403 并留审计：WAN 不代管厂内

建厂不再要求厂端在线。把建厂码拿到厂内管理端「认领工厂」贴上并设密码即可。
